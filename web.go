package patchbin

import (
	"bytes"
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
	"time"

	"github.com/alecthomas/chroma/v2"
	formatterHtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
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
	Name          string
	RepoName      string
	Status        Status
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

// converts contents of files in git tree to pretty formatted code.
func parseText(formatter *formatterHtml.Formatter, theme *chroma.Style, text string) (string, error) {
	lexer := lexers.Get("diff")
	iterator, err := lexer.Tokenise(nil, text)
	if err != nil {
		return text, err
	}
	var buf bytes.Buffer
	err = formatter.Format(&buf, theme, iterator)
	if err != nil {
		return text, err
	}
	return buf.String(), nil
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
	TabStatusDraft    TabStatus = "draft"
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
		case TabStatusDraft:
			prs, err = web.Pr.GetPatchRequestsByStatus(StatusDraft)
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
				Name:          pr.Name,
				RepoName:      pr.RepoName,
				Status:        pr.Status,
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

		// Don't send RSS notifications for draft PRs
		if pr.Status == StatusDraft {
			continue
		}

		displayName := web.Backend.ComputeUserName(user.Pubkey)
		realUrl := fmt.Sprintf("%s/prs/%d", web.Backend.Cfg.Url, eventLog.PatchRequestID.Int64)
		content := fmt.Sprintf(
			"<div><div>Repo: %s</div><div>PatchRequestID: %d</div><div>Event: %s</div><div>Created: %s</div><div>Data: %s</div></div>",
			pr.RepoName,
			eventLog.PatchRequestID.Int64,
			eventLog.Event,
			eventLog.CreatedAt.Format(time.RFC3339Nano),
			eventLog.Data,
		)

		title := fmt.Sprintf(
			`%s in %s for PR "%s" (#%d)`,
			eventLog.Event,
			pr.RepoName,
			pr.Name,
			eventLog.PatchRequestID.Int64,
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
	mux.HandleFunc("GET /prs/active", ctxMdw(ctx, createPrListHandler("active")))
	mux.HandleFunc("GET /prs/draft", ctxMdw(ctx, createPrListHandler("draft")))
	mux.HandleFunc("GET /prs/inactive", ctxMdw(ctx, createPrListHandler("inactive")))
	mux.HandleFunc("GET /prs/{id}", ctxMdw(ctx, createPrDetail("pr")))
	mux.HandleFunc("GET /prs/{id}/patches/{patchID}", ctxMdw(ctx, createPrDetail("pr")))
	mux.HandleFunc("GET /prs/{id}/rss", ctxMdw(ctx, rssHandler))
	mux.HandleFunc("GET /ps/{id}", ctxMdw(ctx, createPrDetail("ps")))
	mux.HandleFunc("GET /ps/{id}/patches/{patchID}", ctxMdw(ctx, createPrDetail("ps")))
	mux.HandleFunc("GET /rss", ctxMdw(ctx, rssHandler))

	mux.HandleFunc("GET /", ctxMdw(ctx, indexHandler))
	mux.HandleFunc("GET /syntax.css", ctxMdw(ctx, chromaStyleHandler))
	embedFS, err := getEmbedFS(embedStaticFS, "static")
	if err != nil {
		panic(err)
	}
	userFS := getUserDefinedFS(cfg.DataDir, "static")

	mux.HandleFunc("GET /static/{file}", ctxMdw(ctx, serveFile(userFS, embedFS)))
	return mux
}
