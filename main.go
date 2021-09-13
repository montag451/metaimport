package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
)

var htmlTemplate = template.Must(template.New("").Parse(`
<html>
  <head>
    <meta name="go-import" content="{{ .Name }} {{ .VCS }} {{ .Repo }}">
  </head>
  <body>
  </body>
</html>
`))

type config struct {
	Host  string
	Port  uint16
	Tls   *tls
	Paths []importPath
}

type tls struct {
	Cert    string
	PrivKey string `json:"priv_key"`
}

type importPath struct {
	Prefix       string
	VCS          string
	RepoTemplate string `json:"repo_template"`
}

type pkgInfo struct {
	Name string
	VCS  string
	Repo string
}

func parseConfig(r io.Reader) *config {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	var conf config
	if err := decoder.Decode(&conf); err != nil {
		switch err.(type) {
		case *json.SyntaxError:
			err := err.(*json.SyntaxError)
			log.Fatalf("conf: syntax error at pos %d: %s", err.Offset, err)
		case *json.UnmarshalTypeError:
			err := err.(*json.UnmarshalTypeError)
			log.Fatalln("conf: bad configuration file", err)
		default:
			log.Fatalf("conf: %s", err)
		}
	}
	return &conf
}

func handler(conf *config, w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("go-get") != "1" {
		log.Printf("not a go-get query %q", r.URL.String())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	pkgName := r.Host + r.URL.Path
	log.Printf("request for %q", pkgName)
	var p *importPath
	pl := 0
	for i, path := range conf.Paths {
		if strings.HasPrefix(pkgName, path.Prefix) && len(path.Prefix) >= pl {
			p = &conf.Paths[i]
			pl = len(path.Prefix)
		}
	}
	if p == nil {
		log.Printf("unable to match package %q", pkgName)
		http.NotFound(w, r)
		return
	}
	repo := &strings.Builder{}
	tmpl := template.New("")
	tmpl.Funcs(template.FuncMap{
		"join": func(elems []string) string {
			return path.Join(elems...)
		},
	})
	tmpl = template.Must(tmpl.Parse(p.RepoTemplate))
	components := strings.Split(pkgName, "/")
	if err := tmpl.Execute(repo, components); err != nil {
		log.Println(err)
		http.NotFound(w, r)
		return
	}
	pkgInfo := pkgInfo{
		Name: pkgName,
		VCS:  p.VCS,
		Repo: repo.String(),
	}
	html := &strings.Builder{}
	if err := htmlTemplate.Execute(html, pkgInfo); err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html.String()))
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s CONF_FILE", os.Args[0])
	}
	confFile, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	conf := parseConfig(confFile)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handler(conf, w, r)
	})
	addr := net.JoinHostPort(conf.Host, fmt.Sprintf("%d", conf.Port))
	if conf.Tls == nil {
		err = http.ListenAndServe(addr, nil)
	} else {
		err = http.ListenAndServeTLS(addr, conf.Tls.Cert, conf.Tls.PrivKey, nil)
	}
	if err != nil {
		log.Fatal(err)
	}
}
