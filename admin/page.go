package admin

import (
	_ "embed"
	"net/http"
)

//go:embed admin.html
var pageHTML []byte

func ServePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pageHTML)
}
