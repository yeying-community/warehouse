package handler

import (
	"net/http"
	"net/url"
	"strings"
)

func setAttachmentContentDisposition(w http.ResponseWriter, fileName string) {
	encodedName := url.PathEscape(fileName)
	fallbackName := asciiFilenameFallback(fileName)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+fallbackName+"\"; filename*=UTF-8''"+encodedName)
}

func asciiFilenameFallback(fileName string) string {
	if fileName == "" {
		return "download"
	}

	var b strings.Builder
	b.Grow(len(fileName))

	for _, r := range fileName {
		switch {
		case r < 0x20 || r > 0x7e:
			b.WriteByte('_')
		case r == '"' || r == '\\' || r == '/' || r == ';':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}

	out := strings.TrimSpace(b.String())
	if out == "" {
		return "download"
	}
	return out
}
