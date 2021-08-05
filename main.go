package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	loc_en "github.com/go-playground/locales/en"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	trans_en "github.com/go-playground/validator/v10/translations/en"
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

type duration time.Duration

func (d *duration) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = duration(value)
		return nil
	case string:
		var err error
		tmp, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = duration(tmp)
		return nil
	default:
		return errors.New("invalid duration")
	}
}

type config struct {
	Addr        string       `json:"addr" validate:"hostname_port"`
	ReadTimeout duration     `json:"read_timeout" validate:"min=1s"`
	TLS         *tls         `json:"tls"`
	Paths       []importPath `json:"paths" validate:"min=1,dive"`
}

type tls struct {
	Cert    string `json:"cert" validate:"required,file"`
	PrivKey string `json:"priv_key" validate:"required,file"`
}

type importPath struct {
	Prefix       string `json:"prefix" validate:"required"`
	VCS          string
	RepoTemplate string `json:"repo_template" validate:"required"`
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
	for _, path := range conf.Paths {
		if strings.HasPrefix(pkgName, path.Prefix) {
			p = &path
			break
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
	v := validator.New()
	trans := ut.New(loc_en.New())
	trans_en.RegisterDefaultTranslations(v, trans.GetFallback())
	v.RegisterTranslation("file", trans.GetFallback(), func(ut ut.Translator) error {
		return ut.Add("file", "{0} does not exist or is not accessible", true)
	}, func(ut ut.Translator, fe validator.FieldError) string {
		s, _ := ut.T("file", fe.Value().(string))
		return s
	})
	if err := v.Struct(conf); err != nil {
		var vErr validator.ValidationErrors
		if errors.As(err, &vErr) {
			log.Println("some errors were found in the configuration file:")
			for _, err := range vErr {
				fmt.Printf("%s: %s\n", err.Namespace(), err.Translate(trans.GetFallback()))
			}
			log.Fatalf("invalid configuration file %q", os.Args[1])
		}
		panic(err)
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handler(conf, w, r)
	})
	server := http.Server{
		Addr:        conf.Addr,
		ReadTimeout: time.Duration(conf.ReadTimeout),
	}
	if conf.TLS == nil {
		err = server.ListenAndServe()
	} else {
		err = server.ListenAndServeTLS(conf.TLS.Cert, conf.TLS.PrivKey)
	}
	if err != nil {
		log.Fatal(err)
	}
}
