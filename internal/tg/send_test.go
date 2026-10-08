package tg

import "testing"

func TestToHTML(t *testing.T) {
	cases := map[string]string{
		"*Hello* _there_ ~old~":        "<b>Hello</b> <i>there</i> <s>old</s>",
		"a*b*c and 5*3*2":              "a*b*c and 5*3*2",
		"see https://x.com/a_b_c/ now": "see https://x.com/a_b_c/ now",
		"`x_y` and _it_":               "<code>x_y</code> and <i>it</i>",
		"> quoted *line*":              "<blockquote>quoted <b>line</b></blockquote>",
		"```code *not bold*```":        "<pre>code *not bold*</pre>",
		"<script> & stuff":             "&lt;script&gt; &amp; stuff",
		"line one\n*line two*":         "line one\n<b>line two</b>",
		"3. *8 am*: story":             "3. <b>8 am</b>: story",
	}
	for in, want := range cases {
		if got := ToHTML(in); got != want {
			t.Errorf("ToHTML(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPeer(t *testing.T) {
	for _, j := range []string{"tg:self", "tg:chat:123", "tg:ch:456:-789"} {
		if _, err := Peer(j); err != nil {
			t.Errorf("%s: %v", j, err)
		}
	}
	for _, j := range []string{"tg:ch:1", "123@g.us", "tg:chat:x"} {
		if _, err := Peer(j); err == nil {
			t.Errorf("%s should fail", j)
		}
	}
}

func TestParse(t *testing.T) {
	a, err := Parse("tg:ch:456:-789:t12")
	if err != nil || a.Topic != 12 || a.Story {
		t.Fatalf("topic: %+v %v", a, err)
	}
	a, err = Parse("tg:story:self")
	if err != nil || !a.Story {
		t.Fatalf("story self: %+v %v", a, err)
	}
	a, err = Parse("tg:story:ch:456:-789")
	if err != nil || !a.Story || a.Topic != 0 {
		t.Fatalf("story channel: %+v %v", a, err)
	}
}
