package postgres

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/images"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/security"
	ws "github.com/coder/websocket"
)

type albumPhoto struct {
	ID             string `json:"id"`
	OwnerID        string `json:"owner_id"`
	Published      bool   `json:"published"`
	ImageURL       string `json:"image_url"`
	ThumbnailURL   string `json:"thumbnail_url"`
	ImageBytes     int64  `json:"image_bytes"`
	ThumbnailBytes int64  `json:"thumbnail_bytes"`
}
type photoChatEvent struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Payload   struct {
		Text  string      `json:"text"`
		Code  string      `json:"code"`
		Photo *albumPhoto `json:"photo"`
	} `json:"payload"`
}

func (c *photoClient) getPhotoResource(t *testing.T, token, path string, want int) ([]byte, http.Header) {
	t.Helper()
	r, err := http.NewRequestWithContext(c.ctx, "GET", c.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "museum_session", Value: token})
	}
	resp, err := c.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != want {
		t.Fatalf("GET %s: %d want %d: %s %v", path, resp.StatusCode, want, data, err)
	}
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Request-ID") == "" {
		t.Fatal(resp.Header)
	}
	return data, resp.Header
}
func (c *photoClient) sendPhotoChat(t *testing.T, payload string) string {
	t.Helper()
	c.seq++
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", c.seq)
	if err := c.conn.Write(c.ctx, ws.MessageText, []byte(fmt.Sprintf(`{"type":"chat.send","request_id":%q,"payload":%s}`, id, payload))); err != nil {
		t.Fatal(err)
	}
	return id
}
func (c *photoClient) readPhotoChat(t *testing.T, want string) photoChatEvent {
	t.Helper()
	_, data, err := c.conn.Read(c.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var e photoChatEvent
	if err = json.Unmarshal(data, &e); err != nil || e.Type != want {
		t.Fatalf("want %s: %s %v", want, data, err)
	}
	return e
}
func (c *photoClient) connectPhotoSocket(t *testing.T) {
	t.Helper()
	conn, _, err := ws.Dial(c.ctx, "ws"+strings.TrimPrefix(c.server.URL, "http")+"/api/v1/ws", &ws.DialOptions{HTTPHeader: http.Header{"Origin": {"http://allowed"}, "Cookie": {"museum_session=" + c.token}}})
	if err != nil {
		t.Fatal(err)
	}
	c.conn = conn
	t.Cleanup(func() { _ = conn.CloseNow() })
	c.read(t, "world.snapshot")
}
func TestPhotoAccessAlbumAndChatPublicationIntegration(t *testing.T) {
	c, dir, store := startPhotoClient(t, images.Codec{})
	c.command(t, "world.ready", `{"world_version":"v1","movement_epoch":1}`, "world.ready")
	webp, err := os.ReadFile(filepath.Join("..", "world", "testdata", "static.webp"))
	if err != nil {
		t.Fatal(err)
	}
	originals := [][]byte{captureJPEG(t), webp}
	photos := make([]albumPhoto, 0, 2)
	for _, data := range originals {
		status, body, e := c.post(security.Crypto{}.NewID(), data)
		if e != nil || status != 201 {
			t.Fatal(status, string(body), e)
		}
		var p albumPhoto
		if e = json.Unmarshal(body, &p); e != nil || p.Published {
			t.Fatal(string(body), e)
		}
		photos = append(photos, p)
	}
	// Register a second player through the public API; it has no game socket yet.
	req, err := http.NewRequestWithContext(c.ctx, "POST", c.server.URL+"/api/v1/auth/register", strings.NewReader(`{"username":"album_reader","display_name":"Reader","password":"password123"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://allowed")
	resp, err := c.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		t.Fatal(resp.StatusCode)
	}
	otherToken := ""
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "museum_session" {
			otherToken = cookie.Value
		}
	}
	_ = resp.Body.Close()
	if otherToken == "" {
		t.Fatal("missing session")
	}
	// All GETs work after closing the owner's socket and before opening the reader's socket.
	_ = c.conn.CloseNow()
	for i, p := range photos {
		for _, thumbnail := range []bool{false, true} {
			url, name, size, mime := p.ImageURL, "image", p.ImageBytes, "image/jpeg"
			if i == 1 {
				mime = "image/webp"
			}
			if thumbnail {
				url, name, size, mime = p.ThumbnailURL, "thumbnail.jpg", p.ThumbnailBytes, "image/jpeg"
			}
			c.getPhotoResource(t, "", url, 401)
			c.getPhotoResource(t, otherToken, url, 404)
			data, headers := c.getPhotoResource(t, c.token, url, 200)
			disk, e := os.ReadFile(filepath.Join(dir, p.ID, name))
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(data, disk) || int64(len(data)) != size || headers.Get("Content-Length") != strconv.FormatInt(size, 10) || headers.Get("Content-Type") != mime || headers.Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("file/DTO mismatch", headers, size)
			}
			if !thumbnail && !bytes.Equal(data, originals[i]) {
				t.Fatal("original changed")
			}
		}
	}
	for _, suffix := range []string{"image", "thumbnail"} {
		c.getPhotoResource(t, c.token, "/api/v1/photos/missing/"+suffix, 404)
	}
	c.getPhotoResource(t, "", "/api/v1/photos", 401)
	album, _ := c.getPhotoResource(t, otherToken, "/api/v1/photos", 200)
	if string(album) != "{\"items\":[],\"next_cursor\":null}\n" {
		t.Fatal("foreign album leaked", string(album))
	}
	album, _ = c.getPhotoResource(t, c.token, "/api/v1/photos?limit=1", 200)
	var page struct {
		Items []albumPhoto `json:"items"`
		Next  *string      `json:"next_cursor"`
	}
	if err = json.Unmarshal(album, &page); err != nil || len(page.Items) != 1 || page.Next == nil || page.Items[0].ID != photos[1].ID {
		t.Fatal(string(album), err)
	}
	c.getPhotoResource(t, otherToken, "/api/v1/photos?cursor="+*page.Next, 400)
	c.getPhotoResource(t, c.token, "/api/v1/photos?cursor=wrong", 400)
	second, _ := c.getPhotoResource(t, c.token, "/api/v1/photos?cursor="+*page.Next, 200)
	if err = json.Unmarshal(second, &page); err != nil || len(page.Items) != 1 || page.Next != nil || page.Items[0].ID != photos[0].ID {
		t.Fatal(string(second), err)
	}
	c.connectPhotoSocket(t) // New socket is deliberately not ready.
	other := &photoClient{ctx: c.ctx, server: c.server, token: otherToken}
	other.connectPhotoSocket(t)
	c.read(t, "player.joined")
	for _, id := range []string{photos[0].ID, "missing"} {
		other.sendPhotoChat(t, fmt.Sprintf(`{"photo_id":%q}`, id))
		if e := other.readPhotoChat(t, "error"); e.Payload.Code != "NOT_FOUND" {
			t.Fatal(e)
		}
	}
	if _, err = store.pool.Exec(c.ctx, `ALTER TABLE photos ADD CONSTRAINT reject_publication CHECK(NOT published)`); err != nil {
		t.Fatal(err)
	}
	c.sendPhotoChat(t, fmt.Sprintf(`{"text":"must not broadcast","photo_id":%q}`, photos[0].ID))
	if e := c.readPhotoChat(t, "error"); e.Payload.Code != "SERVICE_UNAVAILABLE" {
		t.Fatal(e)
	}
	// A sentinel proves failed publications queued no message for either receiver.
	other.sendPhotoChat(t, `{"text":"sentinel"}`)
	for _, client := range []*photoClient{c, other} {
		if e := client.readPhotoChat(t, "chat.message"); e.Payload.Text != "sentinel" || e.Payload.Photo != nil {
			t.Fatal(e)
		}
	}
	c.getPhotoResource(t, otherToken, photos[0].ImageURL, 404)
	if _, err = store.pool.Exec(c.ctx, `ALTER TABLE photos DROP CONSTRAINT reject_publication`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		requestID := c.sendPhotoChat(t, fmt.Sprintf(`{"photo_id":%q}`, photos[0].ID))
		for _, client := range []*photoClient{c, other} {
			e := client.readPhotoChat(t, "chat.message")
			if e.Payload.Text != "" || e.Payload.Photo == nil || !e.Payload.Photo.Published || e.Payload.Photo.ID != photos[0].ID || e.Payload.Photo.ImageBytes != photos[0].ImageBytes || e.Payload.Photo.ThumbnailBytes != photos[0].ThumbnailBytes {
				t.Fatal(e)
			}
			if (client == c && e.RequestID != requestID) || (client == other && e.RequestID != "") {
				t.Fatal("wrong correlation", e)
			}
			// Read immediately upon receiving broadcast: publication must already be committed.
			c.getPhotoResource(t, otherToken, e.Payload.Photo.ImageURL, 200)
		}
	}
	_ = c.conn.CloseNow()
	_ = other.conn.CloseNow()
	for _, url := range []string{photos[0].ImageURL, photos[0].ThumbnailURL} {
		c.getPhotoResource(t, otherToken, url, 200)
		c.getPhotoResource(t, "", url, 401)
	}
	c.getPhotoResource(t, otherToken, photos[1].ImageURL, 404)
	if _, err = store.pool.Exec(c.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE user_id=$1`, c.user); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"/api/v1/photos", photos[0].ImageURL, photos[0].ThumbnailURL} {
		c.getPhotoResource(t, c.token, url, 401)
	}
}
