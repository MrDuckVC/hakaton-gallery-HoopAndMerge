package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/httpapi"
	wsapi "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/websocket"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/worldwire"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/images"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/photofiles"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/sessionevents"
	worldfiles "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/world"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/testworld"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/game"
	photocase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/photo"
	worldcase "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/world"
	ws "github.com/coder/websocket"
)

type photoClient struct {
	ctx         context.Context
	server      *httptest.Server
	conn        *ws.Conn
	token, user string
	seq         int
}

func (c *photoClient) read(t *testing.T, want string) {
	t.Helper()
	_, body, err := c.conn.Read(c.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var e struct{ Type string }
	if err = json.Unmarshal(body, &e); err != nil || e.Type != want {
		t.Fatalf("want %s got %s: %v", want, body, err)
	}
}
func (c *photoClient) command(t *testing.T, kind, payload, want string) {
	t.Helper()
	c.seq++
	body := fmt.Sprintf(`{"type":%q,"request_id":"00000000-0000-4000-8000-%012d","payload":%s}`, kind, c.seq, payload)
	if err := c.conn.Write(c.ctx, ws.MessageText, []byte(body)); err != nil {
		t.Fatal(err)
	}
	c.read(t, want)
}
func (c *photoClient) post(key string, data []byte) (int, []byte, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for name, value := range map[string]string{"room_id": "r0", "movement_epoch": "1", "caption": "  capture  "} {
		if err := w.WriteField(name, value); err != nil {
			return 0, nil, err
		}
	}
	p, err := w.CreateFormFile("image", "capture.jpg")
	if err != nil {
		return 0, nil, err
	}
	if _, err = p.Write(data); err != nil {
		return 0, nil, err
	}
	if err = w.Close(); err != nil {
		return 0, nil, err
	}
	r, err := http.NewRequestWithContext(c.ctx, "POST", c.server.URL+"/api/v1/photos", &b)
	if err != nil {
		return 0, nil, err
	}
	r.Header.Set("Origin", "http://allowed")
	r.Header.Set("Content-Type", w.FormDataContentType())
	r.Header.Set("Idempotency-Key", key)
	r.AddCookie(&http.Cookie{Name: "museum_session", Value: c.token})
	resp, err := c.server.Client().Do(r)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err = io.ReadAll(resp.Body)
	return resp.StatusCode, data, err
}
func startPhotoClient(t *testing.T, codec photocase.Images) (*photoClient, string, *PhotoStore) {
	t.Helper()
	worldDir := t.TempDir()
	if err := testworld.Write(t.Context(), worldDir, "v1"); err != nil {
		t.Fatal(err)
	}
	catalog, err := worldcase.Load(t.Context(), worldfiles.Files{Dir: worldDir}, worldwire.SnapshotSizer{}, "v1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, dsn, pool := testDatabase(t)
	if _, err = Migrate(ctx, dsn, "up"); err != nil {
		t.Fatal(err)
	}
	sessions := auth.New(NewAuthStore(pool), security.Crypto{}, catalog, sessionevents.New(), time.Now)
	connections := game.New(time.Now)
	connections.Configure(catalog, NewGameStore(pool), game.NoQuestions{})
	tr := wsapi.New(connections, sessions, catalog, connections, []string{"http://allowed"})
	api := httpapi.NewServer(":0")
	api.ConfigureAuth(sessions, []string{"http://allowed"}, false)
	api.ConfigureWebSocket(tr)
	dir := t.TempDir()
	files, err := photofiles.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	store := NewPhotoStore(pool)
	api.ConfigurePhotos(photocase.New(sessions, store, files, codec, connections, time.Now, security.Crypto{}.NewID))
	server := httptest.NewServer(api.Handler)
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tr.Shutdown(shutdown)
	})
	login, err := sessions.Register(ctx, "photo_player", "Photo Player", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/ws", &ws.DialOptions{HTTPHeader: http.Header{"Origin": {"http://allowed"}, "Cookie": {"museum_session=" + login.Token}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	client := &photoClient{ctx: ctx, server: server, conn: conn, token: login.Token, user: login.User.ID}
	client.read(t, "world.snapshot")
	return client, dir, store
}
func captureJPEG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 60)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestPhotoUploadHTTPWebSocketPostgresIntegration(t *testing.T) {
	c, dir, store := startPhotoClient(t, images.Codec{})
	imageBytes := captureJPEG(t)
	key := security.Crypto{}.NewID()
	post := func(k string, data []byte, want int) []byte {
		t.Helper()
		status, body, err := c.post(k, data)
		if err != nil || status != want {
			t.Fatalf("status %d want %d body %s err %v", status, want, body, err)
		}
		return body
	}
	post(key, imageBytes, 409)
	c.command(t, "world.ready", `{"world_version":"v1","movement_epoch":1}`, "world.ready")
	type result struct {
		status int
		body   []byte
		err    error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { status, body, err := c.post(key, imageBytes); results <- result{status, body, err} })
	}
	wg.Wait()
	close(results)
	statuses := map[int]int{}
	var id string
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		statuses[r.status]++
		var p struct{ ID, Caption string }
		if err := json.Unmarshal(r.body, &p); err != nil || p.ID == "" || p.Caption != "capture" {
			t.Fatal(string(r.body), err)
		}
		if id != "" && id != p.ID {
			t.Fatal("duplicate IDs")
		}
		id = p.ID
	}
	if statuses[200] != 1 || statuses[201] != 1 {
		t.Fatal(statuses)
	}
	for name, want := range map[string][]byte{"image": imageBytes, "thumbnail.jpg": nil} {
		data, err := os.ReadFile(filepath.Join(dir, id, name))
		if err != nil {
			t.Fatal(err)
		}
		if want != nil && !bytes.Equal(data, want) {
			t.Fatal("original changed")
		}
		if name == "thumbnail.jpg" {
			if _, err = jpeg.Decode(bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	c.command(t, "world.enter", `{"door_id":"d0","movement_epoch":1}`, "world.entered")
	post(key, imageBytes, 200)
	post(key, append(append([]byte{}, imageBytes...), 1), 409)
	post(security.Crypto{}.NewID(), imageBytes, 409)
	post(key, imageBytes, 429)
	var n int
	if err := store.pool.QueryRow(c.ctx, `SELECT count(*) FROM photos`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if err := store.pool.QueryRow(c.ctx, `SELECT count(*) FROM memory_events WHERE type='PHOTO_CREATED'`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}

type blockedPhotoCodec struct{ entered, release chan struct{} }

func (b blockedPhotoCodec) Prepare(ctx context.Context, data []byte) (photocase.Prepared, error) {
	close(b.entered)
	select {
	case <-b.release:
	case <-ctx.Done():
		return photocase.Prepared{}, ctx.Err()
	}
	return (images.Codec{}).Prepare(ctx, data)
}
func TestPhotoUploadRejectsTransitionDuringDecodeIntegration(t *testing.T) {
	codec := blockedPhotoCodec{make(chan struct{}), make(chan struct{})}
	c, dir, store := startPhotoClient(t, codec)
	c.command(t, "world.ready", `{"world_version":"v1","movement_epoch":1}`, "world.ready")
	data := captureJPEG(t)
	done := make(chan int, 1)
	go func() { status, _, _ := c.post(security.Crypto{}.NewID(), data); done <- status }()
	select {
	case <-codec.entered:
	case <-c.ctx.Done():
		t.Fatal(c.ctx.Err())
	}
	c.command(t, "world.enter", `{"door_id":"d0","movement_epoch":1}`, "world.entered")
	close(codec.release)
	if status := <-done; status != 409 {
		t.Fatal(status)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	var n int
	if err = store.pool.QueryRow(c.ctx, `SELECT count(*) FROM photos`).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestPhotoUploadSQLFailureCleansFilesIntegration(t *testing.T) {
	c, dir, store := startPhotoClient(t, images.Codec{})
	c.command(t, "world.ready", `{"world_version":"v1","movement_epoch":1}`, "world.ready")
	if _, err := store.pool.Exec(c.ctx, `ALTER TABLE memory_events ADD CONSTRAINT reject_photo CHECK(type<>'PHOTO_CREATED')`); err != nil {
		t.Fatal(err)
	}
	status, body, err := c.post(security.Crypto{}.NewID(), captureJPEG(t))
	if err != nil || status != 503 {
		t.Fatal(status, string(body), err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	var n int
	if err = store.pool.QueryRow(c.ctx, `SELECT count(*) FROM photos`).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
