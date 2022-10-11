package main

import (
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
)

var mainTemplate = template.Must(template.New("main").Parse(`
{{- /* This is the template used to render the HTML page */ -}}
<html>
  <head>
    <meta name="go-import" content="{{ .Prefix }} {{ .VCS }} {{ .Repo }}">
  </head>
  <body>
  </body>
</html>
`))

func init() {
	mainTemplate.Funcs(template.FuncMap{
		"join": func(elems []string) string {
			return path.Join(elems...)
		},
	})
}

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
	NbComponents int `json:"nb_components"`
	VCS          string
	RepoTemplate string `json:"repo_template"`
}

type metaImport struct {
	Prefix string
	VCS    string
	Repo   string
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

func templateNameForImportPath(i int) string {
	return "path-" + strconv.Itoa(i)
}

func handler(conf *config, w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("go-get") != "1" {
		log.Printf("not a go-get query %q", r.URL.String())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	pkgName := r.Host + r.URL.Path
	components := strings.Split(pkgName, "/")
	log.Printf("request for %q", pkgName)
	var p *importPath
	pi, pl := 0, 0
	for i, path := range conf.Paths {
		if path.NbComponents <= len(components) && strings.HasPrefix(pkgName, path.Prefix) && len(path.Prefix) >= pl {
			p = &conf.Paths[i]
			pi = i
			pl = len(path.Prefix)
		}
	}
	if p == nil {
		log.Printf("unable to match package %q", pkgName)
		http.NotFound(w, r)
		return
	}
	repo := &strings.Builder{}
	tmplName := templateNameForImportPath(pi)
	if err := mainTemplate.ExecuteTemplate(repo, tmplName, components); err != nil {
		log.Printf("failed to execute template for %q: %v", pkgName, err)
		http.NotFound(w, r)
		return
	}
	mi := metaImport{
		Prefix: strings.Join(components[:p.NbComponents], "/"),
		VCS:    p.VCS,
		Repo:   repo.String(),
	}
	html := &strings.Builder{}
	if err := mainTemplate.Execute(html, mi); err != nil {
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
	for i := range conf.Paths {
		p := &conf.Paths[i]
		if p.NbComponents <= 0 {
			p.NbComponents = len(strings.Split(p.Prefix, "/"))
		}
		name := templateNameForImportPath(i)
		template.Must(mainTemplate.New(name).Parse(p.RepoTemplate))
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handler(conf, w, r)
	})
	addr := net.JoinHostPort(conf.Host, strconv.FormatUint(uint64(conf.Port), 10))
	if conf.Tls == nil {
		err = http.ListenAndServe(addr, nil)
	} else {
		err = http.ListenAndServeTLS(addr, conf.Tls.Cert, conf.Tls.PrivKey, nil)
	}
	if err != nil {
		log.Fatal(err)
	}
}
