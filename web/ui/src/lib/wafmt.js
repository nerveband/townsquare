// Render WhatsApp text formatting as safe HTML, the way WhatsApp shows it:
// *bold*  _italic_  ~strike~  `inline code`  ```monospace block```  > quote
// - bullet / * bullet / 1. numbered lines, and links. Everything is escaped first.

const esc = (s) => s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c])

// A marker pair must hug the text (no space just inside) and sit at word edges.
function inline(t) {
  const edge = '(^|[\\s([{"\'“‘])'
  const end = '(?=$|[\\s.,!?:;)\\]}"\'”’…-])'
  const pair = (m, tag) => {
    const q = m.replace(/[*~]/g, '\\$&')
    const re = new RegExp(`${edge}${q}([^\\s${q}](?:[^${q}\\n]*?[^\\s${q}])?)${q}${end}`, 'g')
    t = t.replace(re, `$1<${tag}>$2</${tag}>`)
  }
  pair('*', 'b')
  pair('_', 'i')
  pair('~', 's')
  return t
}

const URL_RE = /\bhttps?:\/\/[^\s<]+[^\s<.,!?:;)\]'"”’]/g

export function waHTML(text) {
  if (!text) return ''
  const parts = esc(text).split(/```/)
  return parts
    .map((chunk, i) => {
      if (i % 2 === 1) return `<pre class="wa-pre">${chunk.replace(/^\n/, '')}</pre>`
      // inline code first, protected from other formatting
      const codes = []
      chunk = chunk.replace(/`([^`\n]+)`/g, (_, c) => { codes.push(c); return `\u0000${codes.length - 1}\u0000` })
      const lines = chunk.split('\n').map((line) => {
        let cls = ''
        let body = line
        let m
        if ((m = line.match(/^&gt; ?(.*)$/))) { cls = 'wa-quote'; body = m[1] }
        else if ((m = line.match(/^[-*•] (.*)$/))) { cls = 'wa-li'; body = '• ' + m[1] }
        else if ((m = line.match(/^(\d+)\. (.*)$/))) { cls = 'wa-ol'; body = `<span class="wa-n">${m[1]}.</span> ${m[2]}` }
        // links are protected from formatting (underscores in URLs stay literal)
        const urls = []
        body = body.replace(URL_RE, (u) => { urls.push(u); return `\u0001${urls.length - 1}\u0001` })
        body = inline(body).replace(/\u0001(\d+)\u0001/g, (_, n) => `<a href="${urls[n]}" target="_blank" rel="noreferrer noopener">${urls[n]}</a>`)
        return cls ? `<span class="${cls}">${body}</span>` : body
      })
      return lines.join('\n').replace(/\u0000(\d+)\u0000/g, (_, n) => `<code class="wa-code">${codes[n]}</code>`)
    })
    .join('')
}

/** Plain text without the markers, for one-line titles and lists. */
export function waPlain(text) {
  return (text || '').replace(/```/g, '').replace(/(^|\s)[*_~]([^*_~\n]+)[*_~](?=\s|$|[.,!?:;])/g, '$1$2').replace(/`/g, '')
}
