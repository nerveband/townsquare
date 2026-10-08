package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nerveband/townsquare/internal/store"
	"github.com/nerveband/townsquare/internal/wa"
)

// Demo mode: sample groups and posts for trying the app and taking screenshots.
// It never connects to WhatsApp and the send loop never runs.

var demoTargets = []store.Target{
	{JID: "10000000001@s.whatsapp.net", Kind: "self", Name: "Message yourself", CanSend: true},
	{JID: "status@broadcast", Kind: "status", Name: "My Status", CanSend: true},
	{JID: "120363000000000101@g.us", Kind: "announce", Name: "Riverside Community", Parent: "Riverside Community", CanSend: true, Members: 842},
	{JID: "120363000000000102@g.us", Kind: "group", Name: "Main Group", CanSend: true, Members: 236},
	{JID: "120363000000000103@g.us", Kind: "group", Name: "Youth Group", CanSend: true, Members: 74},
	{JID: "120363000000000104@g.us", Kind: "group", Name: "Volunteers", CanSend: true, Members: 41},
	{JID: "120363000000000105@g.us", Kind: "group", Name: "Book Club", CanSend: true, Members: 18},
	{JID: "120363000000000106@g.us", Kind: "group", Name: "Sunrise Bakery Regulars", CanSend: true, Members: 129},
	{JID: "120363000000000107@newsletter", Kind: "channel", Name: "Center Updates", CanSend: true, Members: 1310},
}

var demoTelegram = []store.Target{
	{JID: "tg:ch:900000001:1", Kind: "channel", Name: "Riverside on Telegram", CanSend: true, Members: 2140},
}

// SeedDemo fills an empty database with sample data around today.
func (s *Server) SeedDemo(ctx context.Context) error {
	if ps, _ := s.DB.Posts(ctx); len(ps) > 0 {
		return nil
	}
	if err := s.DB.UpsertTargets(ctx, "whatsapp", demoTargets); err != nil {
		return err
	}
	if err := s.DB.UpsertTargets(ctx, "telegram", demoTelegram); err != nil {
		return err
	}
	tz := s.DB.Setting(ctx, "timezone")
	l, _ := time.LoadLocation(tz)
	now := time.Now().In(l)
	monday := now.AddDate(0, 0, -((int(now.Weekday()) + 6) % 7))
	day := func(offset int, hm string) string {
		return monday.AddDate(0, 0, offset).Format("2006-01-02") + "T" + hm
	}
	j := func(i int) string { return demoTargets[i].JID }

	_, _ = s.DB.ExecContext(ctx, `UPDATE targets SET allowed=1`)
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO clients(name,color) VALUES('Riverside Center','#128C7E'),('Sunrise Bakery','#D9962B')`)
	_, _ = s.DB.ExecContext(ctx, `UPDATE targets SET client_id=1 WHERE jid IN (?,?,?,?,?,?)`, j(2), j(3), j(4), j(5), j(6), j(8))
	_, _ = s.DB.ExecContext(ctx, `UPDATE targets SET client_id=2 WHERE jid=?`, j(7))
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO target_sets(name,client_id,jids) VALUES('All Riverside groups',1,?)`,
		fmt.Sprintf(`["%s","%s","%s","%s"]`, j(3), j(4), j(5), j(6)))

	media := map[string]int64{}
	for _, m := range []struct{ key, kind, file string }{
		{"flyer", "image", "weekly-program.jpg"}, {"bake", "image", "bake-sale.jpg"}, {"photos", "image", "month-in-photos.jpg"},
		{"recap", "video", "event-recap.mp4"}, {"voice", "voice", "volunteer-call.m4a"}, {"agenda", "document", "board-agenda.pdf"},
	} {
		path, err := makeDemoFile(ctx, s.DataDir, m.key, m.file)
		if errors.Is(err, exec.ErrNotFound) {
			log.Println("demo: ffmpeg isn't installed, so the sample posts have no photos, videos or voice notes")
			break
		}
		if err != nil {
			return fmt.Errorf("demo media %s: %w", m.file, err)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		md, err := s.ingestMedia(ctx, m.file, "", m.kind, f)
		f.Close()
		if err != nil {
			return err
		}
		media[m.key] = md.ID
	}

	id := func(n int64) *int64 { return &n }
	posts := []store.Post{
		{Title: "Morning reminder", Caption: "Good morning everyone! Today's schedule is pinned in the group description.", Targets: []string{j(3), j(4), j(5)},
			TagID: id(2), ClientID: id(1), Status: "scheduled", Schedules: []store.Schedule{{Start: day(-7, "08:00"), TZ: tz, RRule: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"}}},
		{Title: "Weekly program flyer", Caption: "*This week at the center* 🌙\nTuesday: Youth night, 7pm\nThursday: Family dinner, 6:30pm", Media: []int64{media["flyer"]},
			Targets: []string{j(2), j(3), j(8), demoTelegram[0].JID}, TagID: id(1), ClientID: id(1), Status: "scheduled", Schedules: []store.Schedule{{Start: day(-5, "18:30"), TZ: tz, RRule: "FREQ=WEEKLY;BYDAY=WE"}}},
		{Title: "Friday Status", Caption: "Have a great weekend! Doors open at 10am Saturday.", Targets: []string{j(1)}, TagID: id(5), ClientID: id(1),
			Status: "scheduled", Schedules: []store.Schedule{{Start: day(-3, "11:00"), TZ: tz, RRule: "FREQ=WEEKLY;BYDAY=FR"}}},
		{Title: "Volunteer call", Caption: "Quick voice note: we need 5 more helpers for Saturday.", Media: []int64{media["voice"]}, Targets: []string{j(5)},
			TagID: id(2), ClientID: id(1), Status: "scheduled", Schedules: []store.Schedule{{Start: day(4, "15:00"), TZ: tz}}},
		{Title: "Bake sale launch", Caption: "*Bake sale this Saturday!* Fresh bread, cookies and pie. Pre-order by Friday.", Media: []int64{media["bake"]},
			Targets: []string{j(7), j(1)}, TagID: id(3), ClientID: id(2), Status: "scheduled", Schedules: []store.Schedule{{Start: day(3, "09:00"), TZ: tz}}},
		{Title: "Bake sale reminder", Caption: "Last call! Pre-orders close tonight at 8.", Targets: []string{j(7)}, TagID: id(3), ClientID: id(2),
			Status: "scheduled", Schedules: []store.Schedule{{Start: day(4, "17:00"), TZ: tz}, {Start: day(5, "08:30"), TZ: tz}}},
		{Title: "Event recap", Caption: "Thank you all for coming! 42 seconds of highlights.", Media: []int64{media["recap"]}, Targets: []string{j(8), j(3), demoTelegram[0].JID},
			TagID: id(4), ClientID: id(1), Status: "scheduled", Schedules: []store.Schedule{{Start: day(0, "10:00"), TZ: tz}}},
		{Title: "Board agenda", Caption: "Agenda for Tuesday's meeting is attached.", Media: []int64{media["agenda"]}, Targets: []string{j(5)}, TagID: id(1), ClientID: id(1),
			Status: "scheduled", Schedules: []store.Schedule{{Start: day(1, "10:00"), TZ: tz}}},
		{Title: "Book club pick", Caption: "This month's book: _The Midnight Library_. We meet on the 28th.", Targets: []string{j(6)}, TagID: id(1), ClientID: id(1),
			Status: "scheduled", Schedules: []store.Schedule{{Start: day(2, "19:00"), TZ: tz}}},
		{Title: "Month in photos", Caption: "A look back at this month, 12 photos.", Media: []int64{media["photos"]}, Targets: []string{j(8), j(1)}, TagID: id(4), ClientID: id(1),
			Status: "scheduled", Schedules: []store.Schedule{{Start: day(6, "17:00"), TZ: tz}}},
		{Title: "Daily digest", Caption: "Salaam everyone! Here's what's going out today:\n\n*This morning*\n1. *8 am*: Instagram and Facebook story, _Volunteer spotlight_\n2. *11 am*: Friday Status, doors open at 10\n\n*This evening*\n- Book club reminder at 7 pm\n- Bake sale pre-orders close at 8 pm\n\n> Reply here if anything looks wrong.\n\nFull list: https://example.com/riverside/schedule\n\n~Old link removed~ ```Ref: RC-2026-10```",
			Targets: []string{j(3)}, TagID: id(1), ClientID: id(1), Status: "scheduled", Schedules: []store.Schedule{{Start: day(0, "07:00"), TZ: tz, RRule: "FREQ=DAILY"}}},
		{Title: "Holiday schedule", Caption: "We're closed Monday for the holiday.", Targets: []string{j(2), j(3)}, TagID: id(1), ClientID: id(1), Status: "draft"},
		{Title: "Thank-you note", Caption: "Thank you to every volunteer this season!", Targets: []string{j(5)}, TagID: id(2), ClientID: id(1), Status: "draft"},
	}
	for i := range posts {
		p := &posts[i]
		var ms []int64 // sample media missing when ffmpeg isn't installed
		for _, m := range p.Media {
			if m != 0 {
				ms = append(ms, m)
			}
		}
		p.Media = ms
		if _, err := s.DB.Mutate(ctx, "you", "created "+p.Title, nil, func(tx *store.Tx) error { return tx.PutPost(p) }); err != nil {
			return err
		}
	}
	// Past sends look delivered, with made-up engagement for the stats board.
	all, _ := s.DB.Posts(ctx, "scheduled")
	s.seedDemoStats(ctx, all, now)
	s.DB.Log(ctx, "sender", "Morning reminder: sent to 3")
	s.DB.Log(ctx, "api:planner-agent", "created draft Holiday schedule")
	return nil
}

// seedDemoStats records past sends with plausible reads, views, reactions and
// replies, plus member history, so the stats board has something to show.
func (s *Server) seedDemoStats(ctx context.Context, posts []*store.Post, now time.Time) {
	rng := rand.New(rand.NewPCG(7, 11))
	targets := map[string]store.Target{}
	for _, t := range s.DB.Targets(ctx) {
		targets[t.JID] = t
	}
	_, _ = s.DB.ExecContext(ctx, `INSERT OR REPLACE INTO settings(key,value) VALUES('stats_people','1')`)
	names := []string{"Amina", "Yusuf", "Sara", "Omar", "Leila", "David", "Grace", "Hassan", "Maria", "Bilal", "Fatima", "Noah", "Zainab", "Ibrahim", "Hana", "Lucas", "Aisha", "Samir", "Emma", "Khalid"}
	emojis := []string{"❤️", "👍", "🙏", "😂", "🎉", "🔥", "😮"}
	byPost := map[int64]*store.Post{}
	for _, p := range posts {
		byPost[p.ID] = p
	}
	n := 0
	for _, o := range store.Expand(posts, now.AddDate(0, 0, -21), time.Now()) {
		for _, jid := range o.Targets {
			s.DB.RecordDelivery(ctx, o.PostID, o.ScheduleID, o.Occ, jid, "sent", "demo", "")
			t := targets[jid]
			if t.Kind == "self" {
				continue
			}
			n++
			kind := "text"
			if len(o.Media) > 0 {
				if m, err := s.DB.Media(ctx, o.Media[0]); err == nil {
					kind = m.Kind
				}
			}
			s.recordStatSend(ctx, o, t, posts, []store.StatMsg{{Platform: t.Platform, ID: fmt.Sprintf("demo-%d", n), Kind: kind}})
			var did int64
			_ = s.DB.QueryRowContext(ctx, `SELECT id FROM deliveries WHERE schedule_id=? AND occ=? AND jid=?`, o.ScheduleID, o.Occ, jid).Scan(&did)
			members := t.Members
			if members == 0 {
				members = 60
			}
			hour := o.At.In(now.Location()).Hour()
			base := map[string]float64{"image": .62, "video": .55, "voice": .48, "document": .4, "text": .5}[kind]
			if hour >= 17 && hour <= 20 {
				base += .12
			}
			if hour < 8 {
				base -= .1
			}
			reach := int(float64(members) * math.Min(.95, base+rng.Float64()*.15))
			sentAt := o.At.Unix() + int64(rng.IntN(40))
			age := now.Unix() - sentAt
			frac := func(h float64) float64 { return 1 - math.Exp(-h/4) }
			if age < 7*86400 {
				reach = int(float64(reach) * frac(float64(age)/3600) / frac(168))
			}
			reacts := int(float64(reach) * (.03 + rng.Float64()*.06))
			replies := int(float64(reach) * (.005 + rng.Float64()*.02))
			emoji := map[string]int{}
			left := reacts
			for i := 0; left > 0; i++ {
				k := min(left, max(1, int(float64(reacts)*[]float64{.45, .25, .12, .08, .05, .03, .02}[i%7])))
				emoji[emojis[i%7]] += k
				left -= k
			}
			ej, _ := json.Marshal(emoji)
			snap := func(h float64) any {
				if float64(age) < h*3600 {
					return nil
				}
				return int(float64(reach) * frac(h) / frac(168))
			}
			views, reads, forwards := 0, reach, 0
			if t.Kind == "channel" {
				views, reads, forwards = reach, 0, reach/40
			}
			_, _ = s.DB.ExecContext(ctx, `UPDATE deliveries SET at=? WHERE id=?`, sentAt, did)
			_, _ = s.DB.ExecContext(ctx, `UPDATE stat_sends SET sent_at=?, reads=?, delivered=?, views=?, forwards=?, replies=?, reactions=?, emoji=?, r1h=?, r6h=?, r24h=?, r7d=? WHERE delivery_id=?`,
				sentAt, reads, min(members, reach+reach/5), views, forwards, replies, reacts, string(ej), snap(1), snap(6), snap(24), snap(168), did)
			if reads > 0 && age < 3*86400 {
				for i := 0; i < min(reads, 120); i++ {
					at := sentAt + int64(-math.Log(1-rng.Float64()*.98)*4*3600)
					who := fmt.Sprintf("%s · +1 555 01%02d", names[i%len(names)], i)
					_, _ = s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO stat_people(delivery_id,person,what,who,at) VALUES(?,?,?,?,?)`, did, fmt.Sprintf("p%d", i), "read", who, at)
					if i < reacts {
						_, _ = s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO stat_people(delivery_id,person,what,emoji,who,at) VALUES(?,?,'react',?,?,?)`, did, fmt.Sprintf("p%d", i), emojis[i%3], who, at+60)
					}
				}
			}
		}
	}
	l := now.Location()
	for d := 30; d >= 0; d-- {
		day := now.AddDate(0, 0, -d).In(l).Format("2006-01-02")
		m := map[string]int{}
		for _, t := range targets {
			if t.Members > 0 {
				m[t.JID] = t.Members - int(float64(t.Members)*.0015*float64(d)) + rng.IntN(3)
			}
		}
		s.DB.StatMembers(ctx, day, m)
	}
	s.DB.StatInsight(ctx, demoTelegram[0].JID, map[string]any{
		"followers": map[string]float64{"current": 2140, "previous": 2061}, "views_per_post": map[string]float64{"current": 1180, "previous": 1034},
		"shares_per_post": map[string]float64{"current": 21, "previous": 17}, "reactions_per_post": map[string]float64{"current": 64, "previous": 59},
		"notifications_on": 0.41,
	})
}

// makeDemoFile draws a sample flyer, video, voice clip or PDF with ffmpeg.
func makeDemoFile(ctx context.Context, dataDir, key, name string) (string, error) {
	dir := filepath.Join(dataDir, "demo-src")
	_ = os.MkdirAll(dir, 0o700)
	out := filepath.Join(dir, name)
	card := func(bg, fg, l1, l2 string) []string {
		return []string{"-f", "lavfi", "-i", "color=c=" + bg + ":s=1200x800", "-frames:v", "1", "-vf",
			"drawtext=text='" + l1 + "':fontcolor=" + fg + ":fontsize=110:x=80:y=h-330," +
				"drawtext=text='" + l2 + "':fontcolor=" + fg + "@0.85:fontsize=52:x=84:y=h-180", "-q:v", "3", out}
	}
	var args []string
	switch key {
	case "flyer":
		args = card("0x075E54", "white", "WEEKLY PROGRAM", "Youth night Tue 7pm  ·  Family dinner Thu")
	case "bake":
		args = card("0xF3E2C2", "0x5A3E0B", "BAKE SALE", "Saturday 9am to 1pm  ·  Pre-order by Friday")
	case "photos":
		args = card("0x2E9BD1", "white", "MONTH IN PHOTOS", "12 moments from this month")
	case "recap":
		args = []string{"-f", "lavfi", "-i", "color=c=0x16242B:s=1280x720:d=6", "-f", "lavfi", "-i", "sine=frequency=330:duration=6",
			"-vf", "drawtext=text='EVENT RECAP':fontcolor=white:fontsize=96:x=(w-text_w)/2:y=(h-text_h)/2", "-shortest",
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", out}
	case "voice":
		args = []string{"-f", "lavfi", "-i", "sine=frequency=220:duration=38", "-af", "volume=0.3,tremolo=f=3:d=0.7", out}
	case "agenda":
		return out, os.WriteFile(out, []byte(demoPDF), 0o600)
	}
	full := append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)
	bin := wa.Tool("ffmpeg")
	if bin == "" {
		return "", exec.ErrNotFound
	}
	if b, err := exec.CommandContext(ctx, bin, full...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(b)))
	}
	return out, nil
}

const demoPDF = "%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
	"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj\n" +
	"4 0 obj<</Length 52>>stream\nBT /F1 28 Tf 72 700 Td (Board meeting agenda) Tj ET\nendstream endobj\n" +
	"5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n"
