package main

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"
)

const (
	ARTICLES_PER_PAGE = 5
	DEFAULT_AUTHOR    = "admin"
)

type entry struct {
	Title   string
	Author  string
	Tags    map[string]bool
	Date    string
	TStamp  int64
	Hash    string
	URI     string
	Content string
}

var reTag = regexp.MustCompile(`<!--([\s\S]+?):([\s\S]*?)-->`)

func dir(path string) string {
	p := strings.Split(path, "/")
	return strings.Join(p[:len(p)-1], "/")
}

func parse(html string) *entry {
	e := &entry{Tags: make(map[string]bool)}
	e.Content = html
	for _, m := range reTag.FindAllStringSubmatch(html, -1) {
		if len(m) != 3 {
			continue
		}
		switch v := strings.TrimSpace(m[2]); strings.TrimSpace(m[1]) {
		case "title":
			e.Title = v
		case "author":
			e.Author = v
		case "tag":
			e.Tags[v] = true
		}
	}
	html = strings.Replace(html, "\r\n", "\n", -1)
	e.Hash = fmt.Sprintf("%x", sha1.Sum([]byte(html)))[:8]
	e.TStamp = time.Now().UnixNano()
	e.Date = time.Now().Format("Mon, 02 Jan 2006 15:04:05")
	if e.Author == "" {
		e.Author = DEFAULT_AUTHOR
	}
	return e
}

func main() {
	os.Mkdir("docs", 0777)
	os.Mkdir("docs/blog", 0777)
	os.Mkdir("docs/author", 0777)
	os.Mkdir("docs/tag", 0777)
	os.Link("style.css", "docs/style.css")
	os.Link("logo.png", "docs/logo.png")

	entries := []*entry{}
	tagsReverse, authorsReverse := map[string][]*entry{}, map[string][]*entry{}
	addReverse := func(m *map[string][]*entry, k string, e *entry) {
		if k == "" {
			return
		} else if (*m)[k] == nil {
			(*m)[k] = []*entry{e}
		} else {
			(*m)[k] = append((*m)[k], e)
		}
	}

	filepath.Walk("raw", func(path string, info os.FileInfo, err error) error {
		if info.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}

		html, err := ioutil.ReadFile(path)
		if err != nil {
			panic(err)
		}

		e := parse(string(html))
		e.URI = path[4:]
		hpath := "docs/blog" + path[3:] + "." + e.Hash
		if _, err := os.Stat(hpath); err == nil {
			log.Println("unmodified content:", path, ", pass")
			h, err := os.Open(hpath)
			if err != nil {
				panic(err)
			}
			if err := json.NewDecoder(h).Decode(e); err != nil {
				panic(err)
			}
			h.Close()
		} else {
			log.Println("new content:", path)
			os.MkdirAll(dir(hpath), 0777)
			h, err := os.Create(hpath)
			if err != nil {
				panic(err)
			}
			json.NewEncoder(h).Encode(e)
			h.Close()
		}

		addReverse(&authorsReverse, e.Author, e)
		for tag := range e.Tags {
			addReverse(&tagsReverse, tag, e)
		}

		entries = append(entries, e)
		return nil
	})

	tmpl, err := template.ParseFiles("template.html")
	if err != nil {
		panic(err)
	}

	renderPages := func(entries []*entry, diskPath, title string) {
		// bigger at front
		sort.Slice(entries, func(i, j int) bool { return entries[i].TStamp > entries[j].TStamp })

		var tPayload struct {
			Title                      string
			CurPage, TotalPages        int
			PrevPageLink, NextPageLink string
			Articles                   []*entry
			ArticleEntered             bool
			Tags                       map[string][]*entry
		}

		count, genPageLink := len(entries), func(idx int) string {
			if idx <= 1 {
				return "index.html"
			}
			return fmt.Sprintf("index%d.html", idx)
		}

		tPayload.TotalPages = int(math.Ceil(float64(count) / ARTICLES_PER_PAGE))
		tPayload.Tags = tagsReverse

		for i := 0; i < count; i += ARTICLES_PER_PAGE {
			tPayload.CurPage = i/ARTICLES_PER_PAGE + 1
			tPayload.Title = title
			tPayload.PrevPageLink = genPageLink(tPayload.CurPage - 1)
			tPayload.NextPageLink = genPageLink(tPayload.CurPage + 1)

			end := int(math.Min(float64(i+ARTICLES_PER_PAGE), float64(count)))
			tPayload.Articles = entries[i:end]

			path := diskPath
			if !strings.HasSuffix(diskPath, ".html") {
				path += genPageLink(tPayload.CurPage)
			} else {
				tPayload.ArticleEntered = true
			}

			log.Println("write:", path)
			idx, err := os.Create(path)
			if err != nil {
				panic(err)
			}
			tmpl.Execute(idx, tPayload)
			idx.Close()
		}
	}

	for tag, entries := range tagsReverse {
		path := "docs/tag/" + tag + "/"
		os.MkdirAll(path, 0777)
		renderPages(entries, path, "#"+tag)
	}

	for author, entries := range authorsReverse {
		path := "docs/author/" + author + "/"
		os.MkdirAll(path, 0777)
		renderPages(entries, path, "@"+author)
	}

	for _, e := range entries {
		os.MkdirAll("docs/blog/"+dir(e.URI), 0777)
		renderPages([]*entry{e}, "docs/blog/"+e.URI, e.Title)
	}

	renderPages(entries, "docs/", "Blog")

	log.Println("listen")
	http.Handle("/", http.FileServer(http.Dir("docs")))
	http.ListenAndServe(":8888", nil)
}
