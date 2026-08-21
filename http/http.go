package fbhttp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io/fs"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/spf13/afero"

	"github.com/rforced/filebrowser/v2/files"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/storage"
)

type modifyRequest struct {
	What            string   `json:"what"`             // Answer to: what data type?
	Which           []string `json:"which"`            // Answer to: which fields?
	CurrentPassword string   `json:"current_password"` // Answer to: user logged password
}

func NewHandler(
	imgSvc ImgService,
	fileCache FileCache,
	uploadCache UploadCache,
	store *storage.Storage,
	server *settings.Server,
	assetsFs fs.FS,
) (http.Handler, error) {
	server.Clean()
	server.CaseInsensitiveFs = files.CaseInsensitive(afero.NewOsFs(), server.Root)

	// Fail startup on a bad trusted-proxy list rather than serving with a
	// silently shorter one: every rate limit is keyed on the client address it
	// decides.
	if err := server.ParseTrustedProxies(); err != nil {
		return nil, err
	}

	r := mux.NewRouter()
	index, static := getStaticHandlers(store, server, assetsFs)

	monkey := func(fn handleFunc, prefix string) http.Handler {
		return handle(fn, prefix, store, server)
	}

	r.PathPrefix("/static").Handler(static)
	r.NotFoundHandler = index

	api := r.PathPrefix("/api").Subrouter()

	policy := tokenPolicy{
		expiration:  server.GetTokenExpirationTime(DefaultTokenExpirationTime),
		maxLifetime: server.GetSessionMaxLifetime(settings.DefaultSessionMaxLifetime),
	}
	api.Handle("/login", monkey(loginHandler(policy), ""))
	api.Handle("/handoff", monkey(handoffHandler(policy), "")).Methods("POST")
	api.Handle("/renew", monkey(renewHandler(policy), ""))
	api.Handle("/logout", monkey(logoutHandler, ""))
	api.Handle("/me", monkey(meHandler, ""))

	users := api.PathPrefix("/users").Subrouter()
	users.Handle("", monkey(usersGetHandler, "")).Methods("GET")
	users.Handle("", monkey(userPostHandler, "")).Methods("POST")
	users.Handle("/{id:[0-9]+}", monkey(userPutHandler, "")).Methods("PUT")
	users.Handle("/{id:[0-9]+}", monkey(userGetHandler, "")).Methods("GET")
	users.Handle("/{id:[0-9]+}", monkey(userDeleteHandler, "")).Methods("DELETE")

	api.PathPrefix("/resources/recursive").Handler(monkey(resourceGetRecursiveHandler, "/api/resources/recursive")).Methods("GET")
	api.PathPrefix("/resources").Handler(monkey(resourceGetHandler, "/api/resources")).Methods("GET")
	api.PathPrefix("/resources").Handler(monkey(resourceDeleteHandler(fileCache), "/api/resources")).Methods("DELETE")
	api.PathPrefix("/resources").Handler(monkey(resourcePostHandler(fileCache), "/api/resources")).Methods("POST")
	api.PathPrefix("/resources").Handler(monkey(resourcePutHandler, "/api/resources")).Methods("PUT")
	api.PathPrefix("/resources").Handler(monkey(resourcePatchHandler(fileCache), "/api/resources")).Methods("PATCH")

	api.PathPrefix("/tus").Handler(monkey(tusPostHandler(uploadCache), "/api/tus")).Methods("POST")
	api.PathPrefix("/tus").Handler(monkey(tusHeadHandler(uploadCache), "/api/tus")).Methods("HEAD", "GET")
	api.PathPrefix("/tus").Handler(monkey(tusPatchHandler(uploadCache), "/api/tus")).Methods("PATCH")
	api.PathPrefix("/tus").Handler(monkey(tusDeleteHandler(uploadCache), "/api/tus")).Methods("DELETE")

	// Registered before /usage: gorilla takes the first matching prefix.
	api.PathPrefix("/usage/breakdown").Handler(monkey(usageBreakdownHandler, "/api/usage/breakdown")).Methods("GET")
	api.PathPrefix("/usage").Handler(monkey(diskUsage, "/api/usage")).Methods("GET")

	api.Handle("/shares", monkey(shareListHandler, "")).Methods("GET")
	api.PathPrefix("/share").Handler(monkey(shareGetsHandler, "/api/share")).Methods("GET")
	api.PathPrefix("/share").Handler(monkey(sharePostHandler, "/api/share")).Methods("POST")
	api.PathPrefix("/share").Handler(monkey(shareDeleteHandler, "/api/share")).Methods("DELETE")

	api.Handle("/settings", monkey(settingsGetHandler, "")).Methods("GET")
	api.Handle("/settings", monkey(settingsPutHandler, "")).Methods("PUT")

	api.PathPrefix("/extract").Handler(monkey(extractCheckHandler, "/api/extract")).Methods("GET")
	api.PathPrefix("/extract").Handler(monkey(extractHandler, "/api/extract")).Methods("POST")

	api.PathPrefix("/converge").Handler(monkey(convergeScanHandler, "/api/converge")).Methods("GET")
	api.PathPrefix("/converge").Handler(monkey(convergeCleanHandler, "/api/converge")).Methods("POST")

	api.PathPrefix("/combine").Handler(monkey(combineViewHandler, "/api/combine")).Methods("GET")
	api.PathPrefix("/combine").Handler(monkey(combineHandler, "/api/combine")).Methods("POST")

	api.PathPrefix("/udf").Handler(monkey(udfInfoHandler, "/api/udf")).Methods("GET")
	api.PathPrefix("/udf").Handler(monkey(udfBuildHandler, "/api/udf")).Methods("POST")

	api.PathPrefix("/h5").Handler(monkey(h5Handler, "/api/h5")).Methods("GET")

	api.PathPrefix("/raw").Handler(monkey(rawHandler, "/api/raw")).Methods("GET")
	api.PathPrefix("/preview/{size}/{path:.*}").
		Handler(monkey(previewHandler(imgSvc, fileCache, server.EnableThumbnails, server.ResizePreview), "/api/preview")).Methods("GET")
	api.PathPrefix("/search").Handler(monkey(searchHandler, "/api/search")).Methods("GET")

	public := api.PathPrefix("/public").Subrouter()
	public.PathPrefix("/dl").Handler(monkey(publicDlHandler, "/api/public/dl/")).Methods("GET")
	public.PathPrefix("/share").Handler(monkey(publicShareHandler, "/api/public/share/")).Methods("GET")

	return securityHeaders(server, stripPrefix(server.BaseURL, r)), nil
}

type cspNonceKey struct{}

func contentSecurityPolicy(nonce, frameAncestors string) string {
	script := "'self' https://www.google.com https://www.gstatic.com"
	if nonce != "" {
		script += " 'nonce-" + nonce + "'"
	}

	if frameAncestors == "" {
		frameAncestors = "'none'"
	}

	return `default-src 'self'; ` +
		`script-src ` + script + `; ` +
		`style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://kit.fontawesome.com https://ka-p.fontawesome.com; ` +
		`font-src 'self' data: https://fonts.gstatic.com https://ka-p.fontawesome.com; ` +
		`img-src 'self' data: blob: https://www.gstatic.com; ` +
		`connect-src 'self' https://www.google.com; ` +
		`frame-src 'self' https://www.google.com; ` +
		`manifest-src 'self' blob:; ` +
		`worker-src 'self' blob:; ` +
		`base-uri 'self'; ` +
		`frame-ancestors ` + frameAncestors + `;`
}

func newCSPNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func cspNonce(r *http.Request) string {
	nonce, _ := r.Context().Value(cspNonceKey{}).(string)
	return nonce
}

func securityHeaders(server *settings.Server, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce, err := newCSPNonce()
		if err != nil {
			log.Printf("ERROR: couldn't generate a CSP nonce: %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Security-Policy", contentSecurityPolicy(nonce, server.FrameAncestors))
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), cspNonceKey{}, nonce)))
	})
}
