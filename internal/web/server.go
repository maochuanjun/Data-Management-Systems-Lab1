// Package web adapts HTTP requests to the domain and persistence layers.
package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"questionnaire/internal/domain"
	"questionnaire/internal/store"
)

// Repository keeps HTTP independent of MongoDB so handlers can be tested
// without replacing the production database with an in-memory implementation.
type Repository interface {
	CreateUser(context.Context, *domain.User) error
	FindUserByName(context.Context, string) (*domain.User, error)
	CreateSession(context.Context, string, string, time.Time) error
	SessionUser(context.Context, string) (*domain.User, error)
	DeleteSession(context.Context, string) error
	ListSurveys(context.Context, string) ([]domain.Survey, error)
	Survey(context.Context, string) (*domain.Survey, error)
	CreateSurvey(context.Context, *domain.Survey) error
	UpdateDraft(context.Context, *domain.Survey, int) error
	Transition(context.Context, string, string, string, string, int) error
	InsertResponse(context.Context, *domain.Response, string) error
	Stats(context.Context, *domain.Survey, int, int) (map[string]any, error)
}

//go:embed static/*
var assets embed.FS

type Server struct {
	repo   Repository
	secure bool
	log    *slog.Logger
}

func New(repo Repository, secure bool) http.Handler {
	s := &Server{repo: repo, secure: secure, log: slog.Default()}
	m := http.NewServeMux()
	m.HandleFunc("POST /api/register", s.register)
	m.HandleFunc("POST /api/login", s.login)
	m.HandleFunc("POST /api/logout", s.logout)
	m.HandleFunc("GET /api/me", s.me)
	m.HandleFunc("GET /api/surveys", s.list)
	m.HandleFunc("POST /api/surveys", s.create)
	m.HandleFunc("GET /api/surveys/{id}", s.get)
	m.HandleFunc("PUT /api/surveys/{id}", s.update)
	m.HandleFunc("POST /api/surveys/{id}/publish", s.publish)
	m.HandleFunc("POST /api/surveys/{id}/close", s.closeSurvey)
	m.HandleFunc("GET /api/surveys/{id}/stats", s.stats)
	m.HandleFunc("GET /api/public/surveys/{id}", s.public)
	m.HandleFunc("POST /api/public/surveys/{id}/next", s.next)
	m.HandleFunc("POST /api/public/surveys/{id}/responses", s.submit)
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "接口不存在") })
	static, _ := fs.Sub(assets, "static")
	m.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(static)))
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			fail(w, http.StatusMethodNotAllowed, "请求方法不受支持")
			return
		}
		data, _ := assets.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
	return s.guard(m)
}

func write(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, message string) {
	write(w, code, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		fail(w, 400, "请使用 application/json 请求")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "请求 JSON 无效或字段不符合接口定义")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "请求只能包含一个 JSON 对象")
		return false
	}
	return true
}
func (s *Server) dbError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, 404, "资源不存在")
	case errors.Is(err, store.ErrDuplicate):
		fail(w, 409, "账号已存在，或本问卷不允许重复提交")
	case errors.Is(err, store.ErrConflict):
		fail(w, 409, "问卷状态或版本已变化，请刷新后重试")
	default:
		s.log.Error("database operation failed", "error", err)
		fail(w, 500, "数据库暂时不可用，请稍后重试")
	}
}
func newID() string { return hex.EncodeToString(randomBytes(24)) }
func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
func digest(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			// Browser requests must be same-origin. Non-browser API tools may omit Origin.
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
					fail(w, 403, "不允许跨站请求")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "不允许跨站请求")
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,32}$`)

func (s *Server) session(w http.ResponseWriter, r *http.Request, u *domain.User) bool {
	token := newID()
	if err := s.repo.CreateSession(r.Context(), digest(token), u.ID, time.Now().Add(7*24*time.Hour)); err != nil {
		s.dbError(w, err)
		return false
	}
	if old, err := r.Cookie("session"); err == nil {
		_ = s.repo.DeleteSession(r.Context(), digest(old.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/", MaxAge: 7 * 24 * 3600, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
	return true
}
func (s *Server) auth(w http.ResponseWriter, r *http.Request) *domain.User {
	c, err := r.Cookie("session")
	if err != nil {
		fail(w, 401, "请先登录")
		return nil
	}
	u, err := s.repo.SessionUser(r.Context(), digest(c.Value))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 401, "登录已过期，请重新登录")
		return nil
	}
	if err != nil {
		s.dbError(w, err)
		return nil
	}
	return u
}
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	c.Username = strings.TrimSpace(c.Username)
	if !usernamePattern.MatchString(c.Username) || len(c.Password) < 8 || len(c.Password) > 72 {
		fail(w, 400, "用户名需为 3–32 位字母、数字或下划线；密码需为 8–72 字节")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(w, 500, "密码处理失败")
		return
	}
	u := &domain.User{ID: newID(), Username: c.Username, PasswordHash: string(hash), CreatedAt: time.Now().UTC()}
	if err := s.repo.CreateUser(r.Context(), u); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			fail(w, 409, "用户名已存在，请选择其他用户名")
			return
		}
		s.dbError(w, err)
		return
	}
	if s.session(w, r, u) {
		write(w, 201, u)
	}
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	if len(c.Password) > 72 {
		fail(w, 401, "用户名或密码错误")
		return
	}
	u, err := s.repo.FindUserByName(r.Context(), strings.TrimSpace(c.Username))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.dbError(w, err)
		return
	}
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(c.Password)) != nil {
		fail(w, 401, "用户名或密码错误")
		return
	}
	if s.session(w, r, u) {
		write(w, 200, u)
	}
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err == nil {
		if err := s.repo.DeleteSession(r.Context(), digest(c.Value)); err != nil {
			s.dbError(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
	write(w, 200, map[string]bool{"ok": true})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	if u := s.auth(w, r); u != nil {
		write(w, 200, u)
	}
}

func (s *Server) owned(w http.ResponseWriter, r *http.Request) *domain.Survey {
	u := s.auth(w, r)
	if u == nil {
		return nil
	}
	survey, err := s.repo.Survey(r.Context(), r.PathValue("id"))
	if err != nil {
		s.dbError(w, err)
		return nil
	}
	if survey.OwnerID != u.ID {
		fail(w, 404, "资源不存在")
		return nil
	}
	return survey
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	u := s.auth(w, r)
	if u == nil {
		return
	}
	surveys, err := s.repo.ListSurveys(r.Context(), u.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 200, surveys)
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	u := s.auth(w, r)
	if u == nil {
		return
	}
	var survey domain.Survey
	if !decode(w, r, &survey) {
		return
	}
	survey.ID = newID()
	survey.OwnerID = u.ID
	survey.Status = "draft"
	survey.Revision = 1
	survey.SchemaVersion = 1
	survey.CreatedAt = time.Now().UTC()
	if survey.Questions == nil {
		survey.Questions = []domain.Question{}
	}
	if err := domain.ValidateSurvey(&survey); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if err := s.repo.CreateSurvey(r.Context(), &survey); err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 201, survey)
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	if survey := s.owned(w, r); survey != nil {
		write(w, 200, survey)
	}
}
func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	old := s.owned(w, r)
	if old == nil {
		return
	}
	if old.Status != "draft" {
		fail(w, 409, "已发布问卷不能修改，请创建新问卷")
		return
	}
	var survey domain.Survey
	if !decode(w, r, &survey) {
		return
	}
	expected := survey.Revision
	survey.ID = old.ID
	survey.OwnerID = old.OwnerID
	survey.CreatedAt = old.CreatedAt
	survey.Status = old.Status
	survey.SchemaVersion = old.SchemaVersion
	if survey.Questions == nil {
		survey.Questions = []domain.Question{}
	}
	if err := domain.ValidateSurvey(&survey); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if err := s.repo.UpdateDraft(r.Context(), &survey, expected); err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 200, survey)
}
func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	survey := s.owned(w, r)
	if survey == nil {
		return
	}
	if survey.Status != "draft" {
		fail(w, 409, "只有草稿可以发布")
		return
	}
	if len(survey.Questions) == 0 {
		fail(w, 400, "请先添加至少一道题目")
		return
	}
	if err := domain.ValidateSurvey(survey); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if survey.Deadline != nil && !survey.Deadline.After(time.Now()) {
		fail(w, 400, "截止时间必须晚于当前时间")
		return
	}
	if err := s.repo.Transition(r.Context(), survey.ID, survey.OwnerID, "draft", "published", survey.Revision); err != nil {
		s.dbError(w, err)
		return
	}
	survey.Status = "published"
	survey.Revision++
	write(w, 200, survey)
}
func (s *Server) closeSurvey(w http.ResponseWriter, r *http.Request) {
	survey := s.owned(w, r)
	if survey == nil {
		return
	}
	if err := s.repo.Transition(r.Context(), survey.ID, survey.OwnerID, "published", "closed", survey.Revision); err != nil {
		s.dbError(w, err)
		return
	}
	survey.Status = "closed"
	survey.Revision++
	write(w, 200, survey)
}
func (s *Server) publicSurvey(w http.ResponseWriter, r *http.Request) *domain.Survey {
	if s.auth(w, r) == nil {
		return nil
	}
	survey, err := s.repo.Survey(r.Context(), r.PathValue("id"))
	if err != nil {
		s.dbError(w, err)
		return nil
	}
	if survey.Status == "draft" {
		fail(w, 404, "问卷尚未发布")
		return nil
	}
	return survey
}
func (s *Server) public(w http.ResponseWriter, r *http.Request) {
	if survey := s.publicSurvey(w, r); survey != nil {
		// Public payload deliberately excludes the creator's account ID.
		write(w, 200, struct {
			ID             string            `json:"id"`
			Title          string            `json:"title"`
			Description    string            `json:"description"`
			AllowAnonymous bool              `json:"allowAnonymous"`
			AllowRepeat    bool              `json:"allowRepeat"`
			Deadline       *time.Time        `json:"deadline"`
			Status         string            `json:"status"`
			Questions      []domain.Question `json:"questions"`
		}{survey.ID, survey.Title, survey.Description, survey.AllowAnonymous, survey.AllowRepeat, survey.Deadline, survey.Status, survey.Questions})
	}
}
func open(w http.ResponseWriter, survey *domain.Survey) bool {
	if survey.Status != "published" || (survey.Deadline != nil && !survey.Deadline.After(time.Now())) {
		fail(w, 409, "问卷已关闭或超过截止时间")
		return false
	}
	return true
}
func (s *Server) next(w http.ResponseWriter, r *http.Request) {
	survey := s.publicSurvey(w, r)
	if survey == nil || !open(w, survey) {
		return
	}
	var body struct {
		QuestionID string `json:"questionId"`
		Answer     any    `json:"answer"`
	}
	if !decode(w, r, &body) {
		return
	}
	for _, q := range survey.Questions {
		if q.ID == body.QuestionID {
			answer, err := domain.ValidateAnswer(q, body.Answer)
			if err != nil {
				fail(w, 400, err.Error())
				return
			}
			write(w, 200, map[string]any{"answer": answer, "nextQuestionId": domain.NextQuestion(survey, q.ID, answer)})
			return
		}
	}
	fail(w, 400, "题目不存在")
}
func (s *Server) submit(w http.ResponseWriter, r *http.Request) {
	u := s.auth(w, r)
	if u == nil {
		return
	}
	var body struct {
		Anonymous bool           `json:"anonymous"`
		Answers   map[string]any `json:"answers"`
	}
	if !decode(w, r, &body) {
		return
	}
	survey, err := s.repo.Survey(r.Context(), r.PathValue("id"))
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !open(w, survey) {
		return
	}
	if body.Anonymous && !survey.AllowAnonymous {
		fail(w, 400, "本问卷不允许匿名提交")
		return
	}
	answers, path, err := domain.ValidateSubmission(survey, body.Answers)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	response := &domain.Response{SchemaVersion: 1, ID: newID(), SurveyID: survey.ID, Anonymous: body.Anonymous, Answers: answers, Path: path, SubmittedAt: time.Now().UTC()}
	if !body.Anonymous {
		response.UserID = u.ID
	}
	dedupe := ""
	if !survey.AllowRepeat {
		dedupe = digest(survey.ID + ":" + u.ID)
	}
	// Admission is checked after receiving and validating the entire body.
	// A response admitted before a concurrent close may finish persisting after it.
	survey, err = s.repo.Survey(r.Context(), survey.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !open(w, survey) {
		return
	}
	if err := s.repo.InsertResponse(r.Context(), response, dedupe); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			fail(w, 409, "你已经提交过这份问卷，本问卷不允许重复填写")
			return
		}
		s.dbError(w, err)
		return
	}
	write(w, 201, map[string]any{"id": response.ID, "submittedAt": response.SubmittedAt})
}
func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	survey := s.owned(w, r)
	if survey == nil {
		return
	}
	offset, limit := 0, 20
	for _, param := range []struct {
		name   string
		target *int
	}{{"offset", &offset}, {"limit", &limit}} {
		if values, present := r.URL.Query()[param.name]; present {
			if len(values) != 1 {
				fail(w, 400, "分页参数只能提供一次")
				return
			}
			value, err := strconv.Atoi(values[0])
			if err != nil || value < 0 || (param.name == "limit" && (value < 1 || value > 50)) {
				fail(w, 400, "offset 必须为非负整数；limit 必须为 1 至 50 的整数")
				return
			}
			*param.target = value
		}
	}
	questionID := r.URL.Query().Get("questionId")
	if questionID != "" {
		found := false
		for _, q := range survey.Questions {
			if q.ID == questionID {
				survey.Questions = []domain.Question{q}
				found = true
				break
			}
		}
		if !found {
			fail(w, 404, "题目不存在")
			return
		}
	}
	result, err := s.repo.Stats(r.Context(), survey, offset, limit)
	if err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 200, result)
}
