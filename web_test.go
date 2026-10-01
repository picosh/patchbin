package patchbin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/picosh/patchbin/util"
)

func TestWebRoutingAndRedirects(t *testing.T) {
	dataDir := util.CreateTmpDir()
	defer func() {
		_ = os.RemoveAll(dataDir)
	}()

	suite := setupTest(dataDir, cfgSingleTenantTmpl)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := GitSshServer(ctx, suite.cfg)
	go func() {
		_ = s.ListenAndServe()
	}()
	time.Sleep(100 * time.Millisecond)

	// Create a PR via SSH: repo=myrepo, slug=feat/auth
	output := suite.userKey.MustCmd(suite.patch, "push myrepo:feat/auth")
	if !strings.Contains(output, "created") {
		t.Fatalf("failed to create PR: %s", output)
	}

	handler := GitWebServer(suite.cfg)

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedHeader map[string]string
		containsBody   string
	}{
		{
			name:           "Home page",
			path:           "/",
			expectedStatus: http.StatusOK,
			containsBody:   "A pastebin for patches",
		},
		{
			name:           "Active PRs page",
			path:           "/active",
			expectedStatus: http.StatusOK,
			containsBody:   "feat/auth",
		},
		{
			name:           "Inactive PRs page",
			path:           "/inactive",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Global RSS feed",
			path:           "/rss",
			expectedStatus: http.StatusOK,
			containsBody:   "xml",
		},
		{
			name:           "Repo page",
			path:           "/myrepo",
			expectedStatus: http.StatusOK,
			containsBody:   "feat/auth",
		},
		{
			name:           "Repo RSS feed",
			path:           "/myrepo/rss",
			expectedStatus: http.StatusOK,
			containsBody:   "xml",
		},
		{
			name:           "PR detail page",
			path:           "/myrepo/feat/auth",
			expectedStatus: http.StatusOK,
			containsBody:   "feat/auth",
		},
		{
			name:           "PR raw mbox patch",
			path:           "/myrepo/feat/auth.patch",
			expectedStatus: http.StatusOK,
			containsBody:   "From ",
		},
		{
			name:           "PR RSS feed",
			path:           "/myrepo/feat/auth/rss",
			expectedStatus: http.StatusOK,
			containsBody:   "xml",
		},
		{
			name:           "Legacy redirect /prs/1",
			path:           "/prs/1",
			expectedStatus: http.StatusMovedPermanently,
			expectedHeader: map[string]string{
				"Location": "/myrepo/feat/auth",
			},
		},
		{
			name:           "Legacy redirect /prs/1.patch",
			path:           "/prs/1.patch",
			expectedStatus: http.StatusMovedPermanently,
			expectedHeader: map[string]string{
				"Location": "/myrepo/feat/auth.patch",
			},
		},
		{
			name:           "Legacy redirect /prs/1/rss",
			path:           "/prs/1/rss",
			expectedStatus: http.StatusMovedPermanently,
			expectedHeader: map[string]string{
				"Location": "/myrepo/feat/auth/rss",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Fatalf("GET %s returned status %d, expected %d. Body: %s", tt.path, rec.Code, tt.expectedStatus, rec.Body.String())
			}

			for header, expVal := range tt.expectedHeader {
				gotVal := rec.Header().Get(header)
				if gotVal != expVal {
					t.Errorf("GET %s header %s = %q, expected %q", tt.path, header, gotVal, expVal)
				}
			}

			if tt.containsBody != "" && !strings.Contains(rec.Body.String(), tt.containsBody) {
				t.Errorf("GET %s body does not contain %q", tt.path, tt.containsBody)
			}
		})
	}
}
