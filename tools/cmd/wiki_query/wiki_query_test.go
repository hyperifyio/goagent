package main_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	testutil "github.com/hyperifyio/goagent/tools/testutil"
)

func TestWikiQuery_TitlesSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/w/api.php", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("action") != "query" || q.Get("prop") != "extracts" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
		  "batchcomplete":"",
		  "query":{
		    "pages":{
		      "123":{
		        "pageid":123,
		        "ns":0,
		        "title":"Golang",
		        "extract":"Go is an open source programming language..."
		      }
		    }
		  }
		}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	bin := testutil.BuildTool(t, "wiki_query")
	stdin := []byte(`{"titles":"Golang","language":"en"}`)
	cmd := exec.Command(bin)
    cmd.Env = append(os.Environ(), "MEDIAWIKI_BASE_URL="+ts.URL, "WIKI_QUERY_ALLOW_LOCAL=1")
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tool error: %v, stderr=%s", err, stderr.String())
	}

	var out struct{
		Pages []struct{
			Title   string `json:"title"`
			URL     string `json:"url"`
			Extract string `json:"extract"`
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("bad json: %v: %s", err, stdout.String())
	}
	if len(out.Pages) != 1 || out.Pages[0].Title != "Golang" {
		t.Fatalf("unexpected pages: %+v", out.Pages)
	}
	if out.Pages[0].Extract == "" {
		t.Fatalf("missing extract")
	}
}

func TestWikiQuery_SearchSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/w/api.php", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("action") != "opensearch" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
		  "golang",
		  ["Go (programming language)", "Gopher"],
		  ["Go is an open source...", "Mascot of Go"],
		  [
		    "https://en.wikipedia.org/wiki/Go_(programming_language)",
		    "https://en.wikipedia.org/wiki/Gopher_(programming_language)"
		  ]
		]`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	bin := testutil.BuildTool(t, "wiki_query")
	stdin := []byte(`{"search":"golang"}`)
	cmd := exec.Command(bin)
    cmd.Env = append(os.Environ(), "MEDIAWIKI_BASE_URL="+ts.URL, "WIKI_QUERY_ALLOW_LOCAL=1")
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tool error: %v, stderr=%s", err, stderr.String())
	}
	var out struct{ Pages []struct{ Title, URL, Extract string } }
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("bad json: %v: %s", err, stdout.String())
	}
	if len(out.Pages) != 2 {
		t.Fatalf("unexpected results: %d", len(out.Pages))
	}
	if out.Pages[0].Title == "" || out.Pages[0].URL == "" {
		t.Fatalf("missing fields in first result: %+v", out.Pages[0])
	}
}
