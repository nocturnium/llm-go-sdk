package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// catalogEntry builds one /models/search item shaped like the live catalog.
func catalogEntry(name, task string, props string) string {
	return fmt.Sprintf(`{"id":"x","name":%q,"description":"d","task":{"name":%q},"created_at":"2026-09-29 09:25:44.258","properties":[%s]}`, name, task, props)
}

// catalogServer serves total entries across pages of the requested size. It
// reports total_count as 334, as the live catalog did, so paging must stop on
// a short page rather than trusting it.
func catalogServer(t *testing.T, entries []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/ai/models/search") {
			t.Errorf("path %s", r.URL.Path)
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		start := (page - 1) * per
		end := min(start+per, len(entries))
		if start > len(entries) {
			start = len(entries)
		}
		fmt.Fprintf(w, `{"result":[%s],"success":true,"errors":[],"result_info":{"count":%d,"page":%d,"per_page":%d,"total_count":334}}`, strings.Join(entries[start:end], ","), end-start, page, per)
	}
}

func liveLikeCatalog() []string {
	entries := []string{
		catalogEntry("@cf/cloudflare/clef-flash", "Text Generation", `{"property_id":"context_window","value":"24576"},{"property_id":"vision","value":"true"},{"property_id":"price","value":[{"unit":"per M input tokens","price":0.038,"currency":"USD"}]}`),
		catalogEntry("@cf/cloudflare/clairic", "Text Generation", `{"property_id":"context_window","value":"65536"}`),
		catalogEntry("@cf/openai/gpt-oss-120b", "Text Generation", `{"property_id":"context_window","value":"128000"}`),
		catalogEntry("@cf/baai/bge-m3", "Text Embeddings", ``),
		catalogEntry("@cf/baai/bge-reranker-base", "Text Classification", ``),
		catalogEntry("@cf/huggingface/distilbert-sst-2-int8", "Text Classification", ``),
		catalogEntry("@cf/black-forest-labs/flux-1-schnell", "Text-to-Image", ``),
		catalogEntry("@cf/meta/m2m100-1.2b", "Translation", ``),
	}
	for i := range 110 {
		entries = append(entries, catalogEntry(fmt.Sprintf("@cf/test/model-%d", i), "Text Generation", ``))
	}
	return entries
}

func TestListModels(t *testing.T) {
	entries := liveLikeCatalog()
	var calls int
	var mu sync.Mutex
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		catalogServer(t, entries)(w, r)
	})
	ctx := context.Background()

	t.Run("pages until a short page", func(t *testing.T) {
		mu.Lock()
		calls = 0
		mu.Unlock()
		out, err := c.ListModels(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Models) != len(entries) {
			t.Fatalf("models %d, want %d", len(out.Models), len(entries))
		}
		mu.Lock()
		got := calls
		mu.Unlock()
		if got != 2 {
			t.Fatalf("pages fetched %d, want 2", got)
		}
	})

	byID := map[string]llms.ModelInfo{}
	out, err := c.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range out.Models {
		byID[m.ID] = m
	}
	t.Run("decision models typed from the adapter table", func(t *testing.T) {
		flash := byID["@cf/cloudflare/clef-flash"]
		if !flash.HasType(llms.ModelTypeDecision) || flash.IsChat() || !flash.IsVision() || flash.ContextLength != 24576 {
			t.Fatalf("clef-flash %+v", flash)
		}
		clairic := byID["@cf/cloudflare/clairic"]
		if !clairic.HasType(llms.ModelTypeDecision) {
			t.Fatal("clairic not a decision model")
		}
	})
	t.Run("task types", func(t *testing.T) {
		chat, emb, rerank, img := byID["@cf/openai/gpt-oss-120b"], byID["@cf/baai/bge-m3"], byID["@cf/baai/bge-reranker-base"], byID["@cf/black-forest-labs/flux-1-schnell"]
		if !chat.IsChat() || chat.ContextLength != 128000 {
			t.Error("chat")
		}
		if !emb.HasType(llms.ModelTypeEmbedding) {
			t.Error("embedding")
		}
		if !rerank.HasType(llms.ModelTypeRerank) {
			t.Error("rerank")
		}
		if !img.HasType(llms.ModelTypeImage) {
			t.Error("image")
		}
		if len(byID["@cf/huggingface/distilbert-sst-2-int8"].Types) != 0 || len(byID["@cf/meta/m2m100-1.2b"].Types) != 0 {
			t.Error("tasks without an SDK interface must have no type")
		}
		if byID["@cf/cloudflare/clef-flash"].CreatedAt.IsZero() {
			t.Error("created_at not parsed")
		}
	})
	t.Run("with limit", func(t *testing.T) {
		out, err := c.ListModels(ctx, llms.WithModelLimit(3))
		if err != nil || len(out.Models) != 3 {
			t.Fatal(out, err)
		}
	})
	t.Run("with type filter", func(t *testing.T) {
		out, err := c.ListModels(ctx, llms.WithModelTypes(llms.ModelTypeDecision))
		if err != nil || len(out.Models) != 2 {
			t.Fatal(out, err)
		}
	})
	t.Run("with cursor", func(t *testing.T) {
		if _, err := c.ListModels(ctx, llms.WithModelCursor("x")); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
		if _, err := c.ListModels(ctx, llms.WithModelLimit(-1)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	})
}

func TestListModels_PageCap(t *testing.T) {
	// A server that always returns a full page must not loop forever.
	full := make([]string, catalogPageSize)
	for i := range full {
		full[i] = catalogEntry(fmt.Sprintf("@cf/x/m%d", i), "Text Generation", ``)
	}
	var calls int
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		fmt.Fprintf(w, `{"result":[%s],"success":true,"errors":[]}`, strings.Join(full, ","))
	})
	if _, err := c.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != maxCatalogPages {
		t.Fatalf("pages %d, want %d", calls, maxCatalogPages)
	}
}

func TestListModels_Unsuccessful(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"result":null,"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`)
	})
	if _, err := c.ListModels(context.Background()); err == nil {
		t.Fatal("unsuccessful envelope accepted")
	}
}

func TestModelInfo(t *testing.T) {
	c := mockClient(t, catalogServer(t, liveLikeCatalog()))
	ctx := context.Background()
	t.Run("exact match", func(t *testing.T) {
		m, err := c.ModelInfo(ctx, "@cf/cloudflare/clef-flash")
		if err != nil || m.ID != "@cf/cloudflare/clef-flash" {
			t.Fatal(m, err)
		}
	})
	t.Run("case-insensitive", func(t *testing.T) {
		m, err := c.ModelInfo(ctx, "@CF/CLOUDFLARE/CLAIRIC")
		if err != nil || m.ID != "@cf/cloudflare/clairic" {
			t.Fatal(m, err)
		}
	})
	t.Run("not found", func(t *testing.T) {
		if _, err := c.ModelInfo(ctx, "@cf/none"); !errors.Is(err, llms.ErrModelNotFound) {
			t.Fatal(err)
		}
	})
}

func TestModelInfo_ReturnsCopies(t *testing.T) {
	c := mockClient(t, catalogServer(t, liveLikeCatalog()))
	a, err := c.ModelInfo(context.Background(), "@cf/cloudflare/clef-flash")
	if err != nil {
		t.Fatal(err)
	}
	a.Types[0] = llms.ModelTypeChat
	b, _ := c.ModelInfo(context.Background(), "@cf/cloudflare/clef-flash")
	if b.Types[0] != llms.ModelTypeDecision {
		t.Fatal("mutating a returned model changed later results")
	}
}

func TestConcurrentListModels(t *testing.T) {
	c := mockClient(t, catalogServer(t, liveLikeCatalog()))
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.ListModels(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestConcurrentModelInfo(t *testing.T) {
	c := mockClient(t, catalogServer(t, liveLikeCatalog()))
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.ModelInfo(context.Background(), "@cf/cloudflare/clairic"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

// TestListModels_ServerCapsPageSize pages on the per_page the server reports,
// so a server that caps pages below the request does not end the listing early.
func TestListModels_ServerCapsPageSize(t *testing.T) {
	entries := liveLikeCatalog()
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		const per = 50
		start := min((page-1)*per, len(entries))
		end := min(start+per, len(entries))
		fmt.Fprintf(w, `{"result":[%s],"success":true,"result_info":{"per_page":%d}}`, strings.Join(entries[start:end], ","), per)
	})
	out, err := c.ListModels(context.Background())
	if err != nil || len(out.Models) != len(entries) {
		t.Fatalf("models %d of %d: %v", len(out.Models), len(entries), err)
	}
}

func TestListModels_LimitWithTypeFilterStopsPaging(t *testing.T) {
	// Two decision models sit on page one; a limit of 1 must not read page two.
	entries := liveLikeCatalog()
	var calls int
	var mu sync.Mutex
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		catalogServer(t, entries)(w, r)
	})
	out, err := c.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeDecision), llms.WithModelLimit(1))
	if err != nil || len(out.Models) != 1 || !out.Models[0].HasType(llms.ModelTypeDecision) || calls != 1 {
		t.Fatalf("models %d, pages %d: %v", len(out.Models), calls, err)
	}
}

func TestListModels_LimitStopsPaging(t *testing.T) {
	entries := liveLikeCatalog()
	var calls int
	var mu sync.Mutex
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		catalogServer(t, entries)(w, r)
	})
	out, err := c.ListModels(context.Background(), llms.WithModelLimit(3))
	if err != nil || len(out.Models) != 3 || calls != 1 {
		t.Fatalf("models %d, pages %d: %v", len(out.Models), calls, err)
	}
}

func TestModelInfo_CreatedAt(t *testing.T) {
	c := mockClient(t, catalogServer(t, liveLikeCatalog()))
	m, err := c.ModelInfo(context.Background(), "@cf/cloudflare/clef-flash")
	if err != nil {
		t.Fatal(err)
	}
	// The live catalog value "2026-09-29 09:25:44.258", fractional seconds included.
	if want := time.Date(2026, 9, 29, 9, 25, 44, 258_000_000, time.UTC); !m.CreatedAt.Equal(want) {
		t.Fatalf("CreatedAt %v, want %v", m.CreatedAt, want)
	}
}
