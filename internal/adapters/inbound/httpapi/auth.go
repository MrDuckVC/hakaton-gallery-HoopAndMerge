package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
	world "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
	auth "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/auth"
)

// Auth is the inbound adapter's application boundary, implemented by auth.Service.
type Auth interface {
	Register(context.Context, string, string, string, string) (auth.LoginResult, error)
	Login(context.Context, string, string, string) (auth.LoginResult, error)
	Logout(context.Context, string) error
	Me(context.Context, string) (auth.MeResult, error)
}

type authRoutes struct {
	service Auth
	origins map[string]struct{}
	secure  bool
	limiter *authLimiter
}

// ConfigureAuth connects auth routes before serving. Origins are compared without normalization.
func (s *Server) ConfigureAuth(service Auth, origins []string, secure bool) {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		allowed[origin] = struct{}{}
	}
	routes := &authRoutes{service: service, origins: allowed, secure: secure, limiter: newAuthLimiter(time.Now)}
	s.Handler = s.common(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || webSocketUpgrade(r) {
			values := r.Header.Values("Origin")
			if len(values) != 1 || values[0] == "" || values[0] == "null" {
				writeError(w, 403, "FORBIDDEN", "Недопустимый Origin", 0)
				return
			}
			if _, ok := routes.origins[values[0]]; !ok {
				writeError(w, 403, "FORBIDDEN", "Недопустимый Origin", 0)
				return
			}
		}
		routes.serve(w, r, s.fallback)
	}))
}

func webSocketUpgrade(r *http.Request) bool {
	for _, value := range r.Header.Values("Upgrade") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "websocket") {
				return true
			}
		}
	}
	return false
}

type userDTO struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarColor string `json:"avatar_color"`
}

func publicUser(user domain.User) userDTO {
	return userDTO{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName, AvatarColor: user.AvatarColor}
}

type vectorDTO struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}
type rotationDTO struct {
	Yaw   float64 `json:"yaw"`
	Pitch float64 `json:"pitch"`
}
type poseDTO struct {
	RoomID   string      `json:"room_id"`
	Position vectorDTO   `json:"position"`
	Rotation rotationDTO `json:"rotation"`
}

func publicPose(state player.State) poseDTO {
	return poseDTO{RoomID: state.RoomID, Position: vectorDTO{X: state.Position.X, Y: state.Position.Y, Z: state.Position.Z}, Rotation: rotationDTO{Yaw: state.Rotation.Yaw, Pitch: state.Rotation.Pitch}}
}

func (a *authRoutes) serve(w http.ResponseWriter, r *http.Request, fallback http.HandlerFunc) {
	switch {
	case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/register"):
		if wait := a.limiter.allow(remoteIP(r.RemoteAddr)); wait > 0 {
			writeError(w, 429, "RATE_LIMITED", "Слишком много попыток", wait)
			return
		}
		a.credentials(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/logout":
		if err := a.service.Logout(r.Context(), sessionToken(r)); err != nil {
			applicationError(w, err)
			return
		}
		http.SetCookie(w, a.cookie("", -1))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/me":
		result, err := a.service.Me(r.Context(), sessionToken(r))
		if err != nil {
			applicationError(w, err)
			return
		}
		writeJSON(w, 200, struct {
			User  userDTO `json:"user"`
			State poseDTO `json:"player_state"`
		}{User: publicUser(result.User), State: publicPose(result.State)})
	default:
		fallback(w, r)
	}
}

func (a *authRoutes) credentials(w http.ResponseWriter, r *http.Request) {
	registration := r.URL.Path == "/api/v1/auth/register"
	fields := []string{"username", "password"}
	if registration {
		fields = append(fields, "display_name")
	}
	input, ok := readStrings(w, r, fields)
	if !ok {
		return
	}
	var result auth.LoginResult
	var err error
	if registration {
		result, err = a.service.Register(r.Context(), input["username"], input["display_name"], input["password"], sessionToken(r))
	} else {
		result, err = a.service.Login(r.Context(), input["username"], input["password"], sessionToken(r))
	}
	if err != nil {
		applicationError(w, err)
		return
	}
	http.SetCookie(w, a.cookie(result.Token, 86400))
	status := http.StatusOK
	if registration {
		status = http.StatusCreated
	}
	writeJSON(w, status, struct {
		User userDTO `json:"user"`
	}{User: publicUser(result.User)})
}

func (a *authRoutes) cookie(token string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: "museum_session", Value: token, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode}
}
func sessionToken(r *http.Request) string {
	cookie, err := r.Cookie("museum_session")
	if err != nil {
		return ""
	}
	return cookie.Value
}
func remoteIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	return host
}

func readStrings(w http.ResponseWriter, r *http.Request, fields []string) (map[string]string, bool) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) {
		writeError(w, 415, "UNSUPPORTED_MEDIA_TYPE", "Требуется JSON UTF-8", 0)
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16*1024))
	if err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			writeError(w, 413, "PAYLOAD_TOO_LARGE", "Запрос слишком большой", 0)
		} else {
			writeError(w, 400, "INVALID_JSON", "Некорректный JSON", 0)
		}
		return nil, false
	}
	if !utf8.Valid(body) || !validUnicodeEscapes(body) {
		writeError(w, 400, "INVALID_JSON", "Некорректный JSON UTF-8", 0)
		return nil, false
	}
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&raw); err != nil {
		writeError(w, 400, "INVALID_JSON", "Некорректный JSON", 0)
		return nil, false
	}
	if decoder.Decode(new(any)) != io.EOF {
		writeError(w, 400, "INVALID_JSON", "Некорректный JSON", 0)
		return nil, false
	}
	input := make(map[string]string, len(fields))
	if raw == nil || len(raw) != len(fields) {
		writeError(w, 400, "VALIDATION_ERROR", "Некорректные поля", 0)
		return nil, false
	}
	for _, field := range fields {
		value, exists := raw[field]
		var text string
		if !exists || len(value) == 0 || value[0] != '"' || json.Unmarshal(value, &text) != nil {
			writeError(w, 400, "VALIDATION_ERROR", "Некорректные поля", 0)
			return nil, false
		}
		input[field] = text
	}
	return input, true
}

func applicationError(w http.ResponseWriter, err error) {
	status, code, message := 500, "INTERNAL_ERROR", "Внутренняя ошибка сервера"
	switch {
	case errors.Is(err, domain.Code("NOT_FOUND")):
		status, code, message = 404, "NOT_FOUND", "Ресурс не найден"
	case errors.Is(err, domain.Code("INVALID_CURSOR")):
		status, code, message = 400, "INVALID_CURSOR", "Некорректный курсор"
	case errors.Is(err, world.VersionMismatch):
		status, code, message = 409, "WORLD_VERSION_MISMATCH", "Версия мира не совпадает"
	case errors.Is(err, world.RoomNotAccessible):
		status, code, message = 403, "ROOM_NOT_ACCESSIBLE", "Зал или ресурс недоступен"
	case errors.Is(err, world.NotReady):
		status, code, message = 409, "GAME_NOT_READY", "Нет активного игрового соединения"
	case errors.Is(err, world.NotFound):
		status, code, message = 404, "NOT_FOUND", "Ресурс не найден"
	case errors.Is(err, domain.Validation):
		status, code, message = 400, "VALIDATION_ERROR", "Некорректные данные"
	case errors.Is(err, domain.Unauthenticated):
		status, code, message = 401, "UNAUTHENTICATED", "Требуется вход"
	case errors.Is(err, domain.InvalidCredentials):
		status, code, message = 401, "INVALID_CREDENTIALS", "Неверное имя пользователя или пароль"
	case errors.Is(err, domain.UsernameTaken):
		status, code, message = 409, "USERNAME_TAKEN", "Имя пользователя занято"
	case errors.Is(err, domain.Unavailable) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		status, code, message = 503, "SERVICE_UNAVAILABLE", "Сервис временно недоступен"
	}
	writeError(w, status, code, message, 0)
}

func writeError(w http.ResponseWriter, status int, code, message string, retry time.Duration) {
	body := errorBody{Code: code, Message: message, RequestID: w.Header().Get("X-Request-ID")}
	if retry > 0 {
		body.RetryAfterMS = int64((retry + time.Millisecond - 1) / time.Millisecond)
		w.Header().Set("Retry-After", strconv.FormatInt(int64((retry+time.Second-1)/time.Second), 10))
	}
	writeJSON(w, status, errorResponse{Error: body})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// validUnicodeEscapes prevents encoding/json's replacement of lone UTF-16 surrogates.
// Valid pairs and escaped backslashes retain their original password meaning.
func validUnicodeEscapes(body []byte) bool {
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			continue
		}
		i++
		if i >= len(body) {
			return false
		}
		if body[i] != 'u' {
			continue
		}
		if i+4 >= len(body) {
			return false
		}
		point, err := strconv.ParseUint(string(body[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if point >= 0xdc00 && point <= 0xdfff {
			return false
		}
		if point < 0xd800 || point > 0xdbff {
			continue
		}
		if i+6 >= len(body) || body[i+1] != '\\' || body[i+2] != 'u' {
			return false
		}
		low, parseErr := strconv.ParseUint(string(body[i+3:i+7]), 16, 16)
		if parseErr != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
