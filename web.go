package patchbin

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	formatterHtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/gorilla/feeds"
)

//go:embed static/*
var embedStaticFS embed.FS

var (
	//go:embed tmpl/*
	tmplFS      embed.FS
	indexTmpl   = getTemplate("index.html")
	prTmpl      = getTemplate("pr.html")
	prsListTmpl = getTemplate("prs.html")
	repoTmpl    = getTemplate("repo.html")
)

type BasicData struct {
	MetaData
}

type MetaData struct {
	URL  string
	Desc template.HTML
	Tab  TabStatus
}

type PrListItem struct {
	ID            int64
	Slug          string
	Name          string
	RepoName      string
	FormattedDate string
	NumPatchsets  int
}

type PrListData struct {
	PRs []PrListItem
	MetaData
}

type WebCtx struct {
	Pr        *PrCmd
	Backend   *Backend
	Formatter *formatterHtml.Formatter
	Logger    *slog.Logger
	Theme     *chroma.Style
}

type ctxWeb struct{}

func getTemplate(page string) *template.Template {
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"sha": shaFn,
	}).ParseFS(
		tmplFS,
		filepath.Join("tmpl", "pages", page),
		filepath.Join("tmpl", "components", "*.html"),
		filepath.Join("tmpl", "base.html"),
	)
	if err != nil {
		panic(err)
	}
	return tmpl.Lookup(page)
}

func getWebCtx(r *http.Request) (*WebCtx, error) {
	data, ok := r.Context().Value(ctxWeb{}).(*WebCtx)
	if data == nil || !ok {
		return data, fmt.Errorf("webCtx not set on `r.Context()` for connection")
	}
	return data, nil
}

func setWebCtx(ctx context.Context, web *WebCtx) context.Context {
	return context.WithValue(ctx, ctxWeb{}, web)
}

func ctxMdw(ctx context.Context, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(w, r.WithContext(ctx))
	}
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	web, err := getWebCtx(r)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("content-type", "text/html")
	err = indexTmpl.Execute(w, BasicData{
		MetaData: MetaData{
			URL:  web.Backend.Cfg.Url,
			Desc: template.HTML(web.Backend.Cfg.Desc),
		},
	})
	if err != nil {
		web.Backend.Logger.Error("cannot execute template", "err", err)
	}
}

type TabStatus string

const (
	TabStatusActive   TabStatus = "active"
	TabStatusInactive TabStatus = "inactive"
)

func createPrListHandler(tab TabStatus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		web, err := getWebCtx(r)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var prs []*PatchRequest
		switch TabStatus(tab) {
		case TabStatusInactive:
			prs, err = web.Pr.GetPatchRequestsInactive()
		case TabStatusActive:
			fallthrough
		default:
			prs, err = web.Pr.GetPatchRequestsActive()
		}
		if err != nil {
			web.Backend.Logger.Error("cannot get patch requests", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		prItems := []PrListItem{}
		for _, pr := range prs {
			patchsets, err := web.Pr.GetPatchsetsByPrID(pr.ID)
			if err != nil {
				patchsets = nil
			}
			prItems = append(prItems, PrListItem{
				ID:            pr.ID,
				Slug:          pr.Slug,
				Name:          pr.Name,
				RepoName:      pr.RepoName,
				FormattedDate: pr.CreatedAt.Format(web.Backend.Cfg.TimeFormat),
				NumPatchsets:  len(patchsets),
			})
		}

		w.Header().Set("content-type", "text/html")
		err = prsListTmpl.Execute(w, PrListData{
			PRs: prItems,
			MetaData: MetaData{
				URL:  web.Backend.Cfg.Url,
				Desc: template.HTML(web.Backend.Cfg.Desc),
				Tab:  TabStatus(tab),
			},
		})
		if err != nil {
			web.Backend.Logger.Error("cannot execute template", "err", err)
		}
	}
}

func shaFn(sha string) string {
	if sha == "" {
		return "(none)"
	}
	return truncateSha(sha)
}

func createRepoPrListHandler(w http.ResponseWriter, r *http.Request) {
	web, err := getWebCtx(r)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	repoName := r.PathValue("repo")
	if repoName == "prs" {
		http.Redirect(w, r, "/active", http.StatusMovedPermanently)
		return
	}
	prs, err := web.Pr.GetPatchRequestsByRepoName(repoName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	prItems := []PrListItem{}
	for _, pr := range prs {
		patchsets, _ := web.Pr.GetPatchsetsByPrID(pr.ID)
		prItems = append(prItems, PrListItem{
			ID:            pr.ID,
			Slug:          pr.Slug,
			Name:          pr.Name,
			RepoName:      pr.RepoName,
			FormattedDate: pr.CreatedAt.Format(web.Backend.Cfg.TimeFormat),
			NumPatchsets:  len(patchsets),
		})
	}

	w.Header().Set("content-type", "text/html")
	err = repoTmpl.Execute(w, struct {
		Name   string
		Branch string
		PRs    []PrListItem
		MetaData
	}{
		Name:   repoName,
		Branch: "main",
		PRs:    prItems,
		MetaData: MetaData{
			URL:  web.Backend.Cfg.Url,
			Desc: template.HTML(web.Backend.Cfg.Desc),
		},
	})
	if err != nil {
		web.Backend.Logger.Error("cannot execute template", "err", err)
	}
}

func repoRssHandler(w http.ResponseWriter, r *http.Request) {
	web, err := getWebCtx(r)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	repoName := r.PathValue("repo")
	if repoName == "prs" {
		http.Redirect(w, r, "/rss", http.StatusMovedPermanently)
		return
	}
	prs, err := web.Pr.GetPatchRequestsByRepoName(repoName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	desc := fmt.Sprintf("Events related to repo %s on %s", repoName, web.Backend.Cfg.Url)
	feed := &feeds.Feed{
		Title:       fmt.Sprintf("%s repo events", repoName),
		Link:        &feeds.Link{Href: fmt.Sprintf("https://%s/%s", web.Backend.Cfg.Url, repoName)},
		Description: desc,
		Author:      &feeds.Author{Name: "git collaboration server"},
		Created:     time.Now(),
	}

	var feedItems []*feeds.Item
	for _, pr := range prs {
		eventLogs, err := web.Pr.GetEventLogsByPrID(pr.ID)
		if err != nil {
			continue
		}
		for _, eventLog := range eventLogs {
			user, err := web.Pr.GetUserByID(eventLog.UserID)
			if err != nil {
				continue
			}
			displayName := web.Backend.ComputeUserName(user.Pubkey)
			realUrl := fmt.Sprintf("https://%s/%s/%s", web.Backend.Cfg.Url, pr.RepoName, pr.Slug)
			content := fmt.Sprintf(
				"<div><div>Repo: %s</div><div>Slug: %s</div><div>Event: %s</div><div>Created: %s</div><div>Data: %s</div></div>",
				pr.RepoName, pr.Slug, eventLog.Event, eventLog.CreatedAt.Format(time.RFC3339Nano), eventLog.Data,
			)
			title := fmt.Sprintf(`%s in %s for PR "%s" (%s:%s)`, eventLog.Event, pr.RepoName, pr.Name, pr.RepoName, pr.Slug)
			item := &feeds.Item{
				Id:          fmt.Sprintf("%d", eventLog.ID),
				Title:       title,
				Link:        &feeds.Link{Href: realUrl},
				Content:     content,
				Created:     eventLog.CreatedAt,
				Description: title,
				Author:      &feeds.Author{Name: displayName},
			}
			feedItems = append(feedItems, item)
		}
	}
	feed.Items = feedItems

	rss, err := feed.ToAtom()
	if err != nil {
		http.Error(w, "Could not generate atom rss feed", http.StatusInternalServerError)
		return
	}
	w.Header().Add("Content-Type", "application/atom+xml; charset=utf-8")
	_, _ = w.Write([]byte(rss))
}

func renderPrRss(w http.ResponseWriter, r *http.Request, web *WebCtx, pr *PatchRequest) {
	desc := fmt.Sprintf(
		"Events related to PR %s:%s on %s",
		pr.RepoName, pr.Slug, web.Backend.Cfg.Url,
	)
	feed := &feeds.Feed{
		Title:       fmt.Sprintf("%s:%s events", pr.RepoName, pr.Slug),
		Link:        &feeds.Link{Href: fmt.Sprintf("https://%s/%s/%s", web.Backend.Cfg.Url, pr.RepoName, pr.Slug)},
		Description: desc,
		Author:      &feeds.Author{Name: "git collaboration server"},
		Created:     time.Now(),
	}

	eventLogs, err := web.Pr.GetEventLogsByPrID(pr.ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var feedItems []*feeds.Item
	for _, eventLog := range eventLogs {
		user, err := web.Pr.GetUserByID(eventLog.UserID)
		if err != nil {
			continue
		}
		displayName := web.Backend.ComputeUserName(user.Pubkey)
		realUrl := fmt.Sprintf("https://%s/%s/%s", web.Backend.Cfg.Url, pr.RepoName, pr.Slug)
		content := fmt.Sprintf(
			"<div><div>Repo: %s</div><div>Slug: %s</div><div>Event: %s</div><div>Created: %s</div><div>Data: %s</div></div>",
			pr.RepoName, pr.Slug, eventLog.Event, eventLog.CreatedAt.Format(time.RFC3339Nano), eventLog.Data,
		)
		title := fmt.Sprintf(`%s in %s for PR "%s" (%s:%s)`, eventLog.Event, pr.RepoName, pr.Name, pr.RepoName, pr.Slug)
		item := &feeds.Item{
			Id:          fmt.Sprintf("%d", eventLog.ID),
			Title:       title,
			Link:        &feeds.Link{Href: realUrl},
			Content:     content,
			Created:     eventLog.CreatedAt,
			Description: title,
			Author:      &feeds.Author{Name: displayName},
		}
		feedItems = append(feedItems, item)
	}
	feed.Items = feedItems

	rss, err := feed.ToAtom()
	if err != nil {
		http.Error(w, "Could not generate atom rss feed", http.StatusInternalServerError)
		return
	}
	w.Header().Add("Content-Type", "application/atom+xml; charset=utf-8")
	_, _ = w.Write([]byte(rss))
}

func redirectLegacyPr(w http.ResponseWriter, r *http.Request) {
	idPath := r.PathValue("id")
	if idPath == "" {
		idPath = r.PathValue("slug")
	}
	if idPath == "" {
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
		return
	}

	if idPath == "active" {
		http.Redirect(w, r, "/active", http.StatusMovedPermanently)
		return
	}
	if idPath == "inactive" {
		http.Redirect(w, r, "/inactive", http.StatusMovedPermanently)
		return
	}

	parts := strings.Split(idPath, "/")
	prIDStr := parts[0]
	revPart := ""
	if dot := strings.LastIndex(prIDStr, "."); dot != -1 {
		revPart = prIDStr[dot:]
		prIDStr = prIDStr[:dot]
	}

	prID, err := strconv.ParseInt(prIDStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	web, err := getWebCtx(r)
	if err != nil {
		http.Error(w, "server error", 500)
		return
	}

	pr, err := web.Pr.GetPatchRequestByID(prID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	remainder := ""
	if len(parts) > 1 {
		remainder = "/" + strings.Join(parts[1:], "/")
	}

	newURL := fmt.Sprintf("/%s/%s%s%s", pr.RepoName, pr.Slug, revPart, remainder)
	http.Redirect(w, r, newURL, http.StatusMovedPermanently)
}

func rssHandler(w http.ResponseWriter, r *http.Request) {
	web, err := getWebCtx(r)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	desc := fmt.Sprintf(
		"Events related to git collaboration server %s",
		web.Backend.Cfg.Url,
	)
	feed := &feeds.Feed{
		Title:       fmt.Sprintf("%s events", web.Backend.Cfg.Url),
		Link:        &feeds.Link{Href: web.Backend.Cfg.Url},
		Description: desc,
		Author:      &feeds.Author{Name: "git collaboration server"},
		Created:     time.Now(),
	}

	var eventLogs []*EventLog
	id := r.PathValue("id")
	pubkey := r.URL.Query().Get("pubkey")

	if id != "" {
		var prID int64
		prID, err = getPrID(id)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		eventLogs, err = web.Pr.GetEventLogsByPrID(prID)
	} else if pubkey != "" {
		user, perr := web.Pr.GetUserByPubkey(pubkey)
		if perr != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		eventLogs, err = web.Pr.GetEventLogsByUserID(user.ID)
	} else {
		eventLogs, err = web.Pr.GetEventLogs()
	}

	if err != nil {
		web.Logger.Error("rss could not get eventLogs", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var feedItems []*feeds.Item
	for _, eventLog := range eventLogs {
		user, err := web.Pr.GetUserByID(eventLog.UserID)
		if err != nil {
			web.Logger.Error("user not found for event log", "id", eventLog.ID, "err", err)
			continue
		}

		pr, err := web.Pr.GetPatchRequestByID(eventLog.PatchRequestID.Int64)
		if err != nil {
			continue
		}

		displayName := web.Backend.ComputeUserName(user.Pubkey)
		realUrl := fmt.Sprintf("https://%s/%s/%s", web.Backend.Cfg.Url, pr.RepoName, pr.Slug)
		content := fmt.Sprintf(
			"<div><div>Repo: %s</div><div>Slug: %s</div><div>Event: %s</div><div>Created: %s</div><div>Data: %s</div></div>",
			pr.RepoName,
			pr.Slug,
			eventLog.Event,
			eventLog.CreatedAt.Format(time.RFC3339Nano),
			eventLog.Data,
		)

		title := fmt.Sprintf(
			`%s in %s for PR "%s" (%s:%s)`,
			eventLog.Event,
			pr.RepoName,
			pr.Name,
			pr.RepoName,
			pr.Slug,
		)
		item := &feeds.Item{
			Id:          fmt.Sprintf("%d", eventLog.ID),
			Title:       title,
			Link:        &feeds.Link{Href: realUrl},
			Content:     content,
			Created:     eventLog.CreatedAt,
			Description: title,
			Author:      &feeds.Author{Name: displayName},
		}

		feedItems = append(feedItems, item)
	}
	feed.Items = feedItems

	rss, err := feed.ToAtom()
	if err != nil {
		web.Logger.Error("could not generate atom rss feed", "err", err)
		http.Error(w, "Could not generate atom rss feed", http.StatusInternalServerError)
	}

	w.Header().Add("Content-Type", "application/atom+xml; charset=utf-8")
	_, err = w.Write([]byte(rss))
	if err != nil {
		web.Logger.Error("write error atom rss feed", "err", err)
	}
}

func chromaStyleHandler(w http.ResponseWriter, r *http.Request) {
	web, err := getWebCtx(r)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	w.Header().Add("content-type", "text/css")
	err = web.Formatter.WriteCSS(w, web.Theme)
	if err != nil {
		web.Backend.Logger.Error("cannot write css file", "err", err)
	}
}

func serveFile(userfs fs.FS, embedfs fs.FS) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		web, err := getWebCtx(r)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		logger := web.Logger

		file := r.PathValue("file")

		logger.Info("serving file", "file", file)
		// merging both embedded fs and whatever user provides
		var reader fs.File
		if userfs == nil {
			reader, err = embedfs.Open(file)
		} else {
			reader, err = userfs.Open(file)
			if err != nil {
				// serve embeded static folder
				reader, err = embedfs.Open(file)
			}
		}

		if err != nil {
			logger.Error(err.Error())
			http.Error(w, "file not found", 404)
			return
		}

		contents, err := io.ReadAll(reader)
		if err != nil {
			logger.Error(err.Error())
			http.Error(w, "file not found", 404)
			return
		}
		contentType := mime.TypeByExtension(filepath.Ext(file))
		if contentType == "" {
			contentType = http.DetectContentType(contents)
		}
		w.Header().Add("Content-Type", contentType)

		_, err = w.Write(contents)
		if err != nil {
			logger.Error(err.Error())
			http.Error(w, "server error", 500)
			return
		}
	}
}

func getUserDefinedFS(datadir, dirName string) fs.FS {
	dir := filepath.Join(datadir, dirName)
	_, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	return os.DirFS(dir)
}

func getEmbedFS(ffs embed.FS, dirName string) (fs.FS, error) {
	fsys, err := fs.Sub(ffs, dirName)
	if err != nil {
		return nil, err
	}
	return fsys, nil
}

func GitWebServer(cfg *GitCfg) http.Handler {
	dbpath := filepath.Join(cfg.DataDir, "pr.db?_fk=on")
	dbh, err := SqliteOpen("file:"+dbpath, cfg.Logger)
	if err != nil {
		panic(fmt.Sprintf("cannot find database file, check folder and perms: %s: %s", dbpath, err))
	}

	be := &Backend{
		DB:     dbh,
		Logger: cfg.Logger,
		Cfg:    cfg,
	}
	prCmd := &PrCmd{
		Backend: be,
	}
	formatter := formatterHtml.New(
		formatterHtml.WithClasses(true),
	)
	web := &WebCtx{
		Pr:        prCmd,
		Backend:   be,
		Logger:    cfg.Logger,
		Formatter: formatter,
		Theme:     styles.Get(cfg.Theme),
	}

	ctx := context.Background()
	ctx = setWebCtx(ctx, web)

	// ensure legacy router is disabled
	// GODEBUG=httpmuxgo121=0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", ctxMdw(ctx, indexHandler))
	mux.HandleFunc("GET /active", ctxMdw(ctx, createPrListHandler("active")))
	mux.HandleFunc("GET /inactive", ctxMdw(ctx, createPrListHandler("inactive")))
	mux.HandleFunc("GET /rss", ctxMdw(ctx, rssHandler))
	mux.HandleFunc("GET /syntax.css", ctxMdw(ctx, chromaStyleHandler))

	// Repo routes
	mux.HandleFunc("GET /{repo}", ctxMdw(ctx, createRepoPrListHandler))

	// PR detail routes (supports branches with slashes, .patch, /rss, /patches/{patchID})
	mux.HandleFunc("GET /{repo}/{slug...}", ctxMdw(ctx, createPrDetail))

	embedFS, err := getEmbedFS(embedStaticFS, "static")
	if err != nil {
		panic(err)
	}
	userFS := getUserDefinedFS(cfg.DataDir, "static")

	mux.HandleFunc("GET /static/{file}", ctxMdw(ctx, serveFile(userFS, embedFS)))
	return mux
}
