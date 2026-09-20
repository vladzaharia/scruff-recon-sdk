package core

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestRequestBuildURL(t *testing.T) {
	tests := []struct {
		name  string
		base  string
		req   Request
		want  string
		query map[string]string
	}{
		{
			name: "path joined to base",
			base: "https://example.test/api",
			req:  Request{Method: "GET", Path: "/messaging/conversations"},
			want: "https://example.test/api/messaging/conversations",
		},
		{
			name: "base trailing slash is not doubled",
			base: "https://example.test/api/",
			req:  Request{Method: "GET", Path: "conversations"},
			want: "https://example.test/api/conversations",
		},
		{
			name:  "query merged onto path query",
			base:  "https://example.test",
			req:   Request{Method: "GET", Path: "/x?a=1", Query: url.Values{"b": {"2"}}},
			query: map[string]string{"a": "1", "b": "2"},
		},
		{
			name:  "query overrides duplicate key",
			base:  "https://example.test",
			req:   Request{Method: "GET", Path: "/x?a=old", Query: url.Values{"a": {"new"}}},
			query: map[string]string{"a": "new"},
		},
		{
			name: "absolute path bypasses base",
			base: "https://example.test/api",
			req:  Request{Method: "GET", Path: "https://cdn.example.test/file.jpg"},
			want: "https://cdn.example.test/file.jpg",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.req.Build(context.Background(), tc.base)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if tc.want != "" && got.URL.String() != tc.want {
				t.Errorf("url = %q, want %q", got.URL, tc.want)
			}
			for k, want := range tc.query {
				if v := got.URL.Query().Get(k); v != want {
					t.Errorf("query %q = %q, want %q", k, v, want)
				}
			}
		})
	}
}

// SetParam is the mechanism that lets one Authenticator serve a network that
// signs via query string on GET and via form body on POST.
func TestSetParamRoutesByMethodAndBody(t *testing.T) {
	t.Run("GET goes to query", func(t *testing.T) {
		r := Request{Method: http.MethodGet, Path: "/x"}
		r.SetParam("device_id", "droid-1")
		if r.Query.Get("device_id") != "droid-1" {
			t.Errorf("query = %v, want device_id in query", r.Query)
		}
		if r.Form != nil {
			t.Errorf("form should be nil, got %v", r.Form)
		}
	})

	t.Run("DELETE goes to query", func(t *testing.T) {
		r := Request{Method: http.MethodDelete, Path: "/x"}
		r.SetParam("device_id", "droid-1")
		if r.Query.Get("device_id") != "droid-1" {
			t.Errorf("want device_id in query, got %v", r.Query)
		}
	})

	t.Run("POST goes to form", func(t *testing.T) {
		r := Request{Method: http.MethodPost, Path: "/x"}
		r.SetParam("device_id", "droid-1")
		if r.Form.Get("device_id") != "droid-1" {
			t.Errorf("want device_id in form, got %v", r.Form)
		}
		if r.Query != nil {
			t.Errorf("query should be nil, got %v", r.Query)
		}
	})

	t.Run("multipart goes to a part", func(t *testing.T) {
		r := Request{Method: http.MethodPost, Path: "/x"}
		r.AddPart("message", "hi")
		r.SetParam("device_id", "droid-1")
		if len(r.Multipart) != 2 || r.Multipart[1].Name != "device_id" {
			t.Errorf("want device_id appended as a part, got %+v", r.Multipart)
		}
	})

	t.Run("JSON body is not disturbed", func(t *testing.T) {
		r := Request{Method: http.MethodPost, Path: "/x", JSON: map[string]string{"text": "hi"}}
		r.SetParam("culture", "en")
		if r.Query.Get("culture") != "en" {
			t.Errorf("want culture in query, got %v", r.Query)
		}
		if r.Form != nil {
			t.Error("must not add a form body alongside JSON")
		}
	})
}

// SCRUFF's chat send uses multipart/mixed and a fixed part order; both need to
// survive materialisation.
func TestMultipartPreservesOrderAndSubtype(t *testing.T) {
	r := Request{Method: http.MethodPost, Path: "/app/chat", MultipartContentType: "multipart/mixed"}
	r.AddFilePart("image", "image.jpg", "image/jpg", []byte{0xff, 0xd8})
	for _, n := range []string{"recipient", "guid", "request_guid", "message_type", "message"} {
		r.AddPart(n, "v-"+n)
	}

	req, err := r.Build(context.Background(), "https://example.test")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	ct := req.Header.Get("Content-Type")
	mediatype, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("parse content type %q: %v", ct, err)
	}
	if mediatype != "multipart/mixed" {
		t.Errorf("media type = %q, want multipart/mixed", mediatype)
	}
	if params["boundary"] == "" {
		t.Fatal("boundary was lost when overriding the subtype")
	}

	mr := multipart.NewReader(req.Body, params["boundary"])
	var order []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		order = append(order, p.FormName())
		if p.FormName() == "image" && p.FileName() != "image.jpg" {
			t.Errorf("file part filename = %q, want image.jpg", p.FileName())
		}
		_, _ = io.Copy(io.Discard, p)
	}

	want := []string{"image", "recipient", "guid", "request_guid", "message_type", "message"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("part order = %v, want %v", order, want)
	}
}

func TestBuildSetsContentType(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{"json", Request{Method: "POST", Path: "/x", JSON: map[string]int{"a": 1}}, "application/json"},
		{"form", Request{Method: "POST", Path: "/x", Form: url.Values{"a": {"1"}}}, "application/x-www-form-urlencoded"},
		{"raw", Request{Method: "PUT", Path: "/x", Raw: []byte("x"), RawContentType: "image/jpeg"}, "image/jpeg"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := tc.req.Build(context.Background(), "https://example.test")
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if got := req.Header.Get("Content-Type"); got != tc.want {
				t.Errorf("Content-Type = %q, want %q", got, tc.want)
			}
		})
	}
}

// Set replaces, Add appends. Both APIs use repeated keys for list-valued
// parameters, and using Set for those silently keeps only the last value —
// which looks like success while doing a fraction of the work.
func TestAddQueryAndAddFormAppend(t *testing.T) {
	var r Request
	r.SetQuery("a", "1")
	r.SetQuery("a", "2")
	if got := r.Query["a"]; len(got) != 1 || got[0] != "2" {
		t.Errorf("SetQuery twice = %v, want it to replace", got)
	}

	r.AddQuery("ids[]", "x")
	r.AddQuery("ids[]", "y")
	if got := r.Query["ids[]"]; len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Errorf("AddQuery = %v, want both values kept", got)
	}

	var f Request
	f.AddForm("ids[]", "x")
	f.AddForm("ids[]", "y")
	if got := f.Form["ids[]"]; len(got) != 2 {
		t.Errorf("AddForm = %v, want both values kept", got)
	}
}

// HasParam must see values added via the appending setters too, or the SCRUFF
// authenticator would add a duplicate identity parameter.
func TestHasParamSeesAddedValues(t *testing.T) {
	var r Request
	r.AddQuery("request_guid", "g")
	if !r.HasParam("request_guid") {
		t.Error("HasParam missed a value added with AddQuery")
	}
	var f Request
	f.AddForm("request_guid", "g")
	if !f.HasParam("request_guid") {
		t.Error("HasParam missed a value added with AddForm")
	}
}
