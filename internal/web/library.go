package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"questionnaire/internal/domain"
)

// The additive capability leaves existing phase-one repository adapters intact.
type LibraryRepository interface {
	FindUser(context.Context, string) (*domain.User, error)
	ListQuestionTemplates(context.Context, string) ([]domain.QuestionTemplate, error)
	QuestionTemplate(context.Context, string) (*domain.QuestionTemplate, error)
	QuestionVersion(context.Context, string) (*domain.QuestionVersion, error)
	QuestionVersions(context.Context, string) ([]domain.QuestionVersion, error)
	CreateQuestionTemplate(context.Context, *domain.QuestionTemplate, *domain.QuestionVersion) error
	CreateQuestionVersion(context.Context, string, string, *domain.QuestionVersion) error
	SetQuestionSharing(context.Context, string, string, []string) error
	ListQuestionBanks(context.Context, string) ([]domain.QuestionBank, error)
	QuestionBank(context.Context, string) (*domain.QuestionBank, error)
	CreateQuestionBank(context.Context, *domain.QuestionBank) error
	UpdateQuestionBank(context.Context, *domain.QuestionBank, int) error
	DeleteQuestionBank(context.Context, string, string, int) error
	QuestionUsage(context.Context, string, string) (map[string]any, error)
	QuestionStats(context.Context, string, string, string, int, int) (map[string]any, error)
}

func (s *Server) libraryRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/questions", s.listQuestions)
	m.HandleFunc("POST /api/questions", s.createQuestion)
	m.HandleFunc("GET /api/questions/{id}", s.getQuestion)
	m.HandleFunc("GET /api/questions/{id}/versions", s.listVersions)
	m.HandleFunc("POST /api/questions/{id}/versions", s.createVersion)
	m.HandleFunc("POST /api/questions/{id}/restore", s.restoreVersion)
	m.HandleFunc("PUT /api/questions/{id}/sharing", s.shareQuestion)
	m.HandleFunc("GET /api/questions/{id}/usage", s.questionUsage)
	m.HandleFunc("GET /api/questions/{id}/stats", s.questionStats)
	m.HandleFunc("GET /api/question-versions/{id}", s.getVersion)
	m.HandleFunc("GET /api/question-banks", s.listBanks)
	m.HandleFunc("POST /api/question-banks", s.createBank)
	m.HandleFunc("GET /api/question-banks/{id}", s.getBank)
	m.HandleFunc("PUT /api/question-banks/{id}", s.updateBank)
	m.HandleFunc("DELETE /api/question-banks/{id}", s.deleteBank)
}

func (s *Server) libraryUser(w http.ResponseWriter, r *http.Request) *domain.User {
	u := s.auth(w, r)
	if u == nil {
		return nil
	}
	if s.library == nil {
		fail(w, 500, "题库服务未配置")
		return nil
	}
	return u
}
func canReadTemplate(t *domain.QuestionTemplate, userID string) bool {
	if t.OwnerID == userID {
		return true
	}
	for _, id := range t.SharedWith {
		if id == userID {
			return true
		}
	}
	return false
}
func (s *Server) accessibleTemplate(w http.ResponseWriter, r *http.Request, id, userID string, ownerOnly bool) *domain.QuestionTemplate {
	t, err := s.library.QuestionTemplate(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return nil
	}
	if !canReadTemplate(t, userID) || (ownerOnly && t.OwnerID != userID) {
		fail(w, 404, "题目不存在或没有权限")
		return nil
	}
	return t
}
func (s *Server) templateNames(w http.ResponseWriter, r *http.Request, t *domain.QuestionTemplate, viewer string) bool {
	if t.OwnerID != viewer {
		t.SharedWith = nil
		t.SharedUsernames = nil
		return true
	}
	t.SharedUsernames = []string{}
	for _, id := range t.SharedWith {
		u, err := s.library.FindUser(r.Context(), id)
		if err != nil {
			s.dbError(w, err)
			return false
		}
		t.SharedUsernames = append(t.SharedUsernames, u.Username)
	}
	return true
}
func validNote(w http.ResponseWriter, note string) bool {
	if utf8.RuneCountInString(note) > 500 {
		fail(w, 400, "版本说明最多500个字符")
		return false
	}
	return true
}

func (s *Server) listQuestions(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	items, err := s.library.ListQuestionTemplates(r.Context(), u.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	for i := range items {
		if !s.templateNames(w, r, &items[i], u.ID) {
			return
		}
	}
	write(w, 200, items)
}
func (s *Server) getQuestion(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	t := s.accessibleTemplate(w, r, r.PathValue("id"), u.ID, false)
	if t == nil {
		return
	}
	if s.templateNames(w, r, t, u.ID) {
		write(w, 200, t)
	}
}
func (s *Server) createQuestion(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	var body struct {
		Question domain.Question `json:"question"`
		Note     string          `json:"note"`
	}
	if !decode(w, r, &body) || !validNote(w, body.Note) {
		return
	}
	if err := domain.ValidateReusableQuestion(&body.Question); err != nil {
		fail(w, 400, err.Error())
		return
	}
	now := time.Now().UTC()
	t := &domain.QuestionTemplate{ID: newID(), OwnerID: u.ID, SharedWith: []string{}, NextVersion: 1, CreatedAt: now}
	v := &domain.QuestionVersion{ID: newID(), QuestionID: t.ID, Version: 1, Question: body.Question, Note: body.Note, CreatedAt: now}
	if err := s.library.CreateQuestionTemplate(r.Context(), t, v); err != nil {
		s.dbError(w, err)
		return
	}
	t.LatestVersion = v
	write(w, 201, t)
}
func (s *Server) listVersions(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	t := s.accessibleTemplate(w, r, r.PathValue("id"), u.ID, false)
	if t == nil {
		return
	}
	versions, err := s.library.QuestionVersions(r.Context(), t.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 200, versions)
}
func (s *Server) getVersion(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	v, err := s.library.QuestionVersion(r.Context(), r.PathValue("id"))
	if err != nil {
		s.dbError(w, err)
		return
	}
	if s.accessibleTemplate(w, r, v.QuestionID, u.ID, false) != nil {
		write(w, 200, v)
	}
}
func (s *Server) createVersion(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	t := s.accessibleTemplate(w, r, r.PathValue("id"), u.ID, true)
	if t == nil {
		return
	}
	var body struct {
		BaseVersionID string          `json:"baseVersionId"`
		Question      domain.Question `json:"question"`
		Note          string          `json:"note"`
	}
	if !decode(w, r, &body) || !validNote(w, body.Note) {
		return
	}
	if err := domain.ValidateReusableQuestion(&body.Question); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if body.BaseVersionID == "" && t.LatestVersion != nil {
		body.BaseVersionID = t.LatestVersion.ID
	}
	parent, err := s.library.QuestionVersion(r.Context(), body.BaseVersionID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if parent.QuestionID != t.ID {
		fail(w, 400, "父版本必须属于当前题目")
		return
	}
	v := &domain.QuestionVersion{ID: newID(), QuestionID: t.ID, ParentVersionID: parent.ID, Question: body.Question, Note: body.Note, CreatedAt: time.Now().UTC()}
	if err := s.library.CreateQuestionVersion(r.Context(), t.ID, u.ID, v); err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 201, v)
}
func (s *Server) restoreVersion(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	t := s.accessibleTemplate(w, r, r.PathValue("id"), u.ID, true)
	if t == nil {
		return
	}
	var body struct {
		VersionID string `json:"versionId"`
		Note      string `json:"note"`
	}
	if !decode(w, r, &body) || !validNote(w, body.Note) {
		return
	}
	old, err := s.library.QuestionVersion(r.Context(), body.VersionID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if old.QuestionID != t.ID {
		fail(w, 400, "恢复版本必须属于当前题目")
		return
	}
	if t.LatestVersion == nil {
		fail(w, 409, "题目没有可用版本")
		return
	}
	v := &domain.QuestionVersion{ID: newID(), QuestionID: t.ID, ParentVersionID: t.LatestVersion.ID, RestoredFromVersionID: old.ID, Question: old.Question, Note: body.Note, CreatedAt: time.Now().UTC()}
	if err := s.library.CreateQuestionVersion(r.Context(), t.ID, u.ID, v); err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 201, v)
}
func (s *Server) shareQuestion(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	t := s.accessibleTemplate(w, r, r.PathValue("id"), u.ID, true)
	if t == nil {
		return
	}
	var body struct {
		Usernames []string `json:"usernames"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.Usernames) > 100 {
		fail(w, 400, "共享用户最多100名")
		return
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, name := range body.Usernames {
		name = strings.TrimSpace(name)
		if !usernamePattern.MatchString(name) {
			fail(w, 400, "共享用户名无效")
			return
		}
		other, err := s.repo.FindUserByName(r.Context(), name)
		if err != nil {
			s.dbError(w, err)
			return
		}
		if other.ID != u.ID && !seen[other.ID] {
			ids = append(ids, other.ID)
			seen[other.ID] = true
		}
	}
	if err := s.library.SetQuestionSharing(r.Context(), t.ID, u.ID, ids); err != nil {
		s.dbError(w, err)
		return
	}
	t.SharedWith = ids
	if s.templateNames(w, r, t, u.ID) {
		write(w, 200, t)
	}
}
func (s *Server) questionUsage(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	t := s.accessibleTemplate(w, r, r.PathValue("id"), u.ID, false)
	if t == nil {
		return
	}
	data, err := s.library.QuestionUsage(r.Context(), t.ID, u.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 200, data)
}
func libraryPage(r *http.Request) (int, int, error) {
	offset, limit := 0, 20
	for _, p := range []struct {
		name string
		to   *int
	}{{"offset", &offset}, {"limit", &limit}} {
		if values, present := r.URL.Query()[p.name]; present {
			if len(values) != 1 {
				return 0, 0, fmt.Errorf("分页参数只能提供一次")
			}
			n, err := strconv.Atoi(values[0])
			if err != nil || n < 0 || (p.name == "limit" && (n < 1 || n > 50)) {
				return 0, 0, fmt.Errorf("offset必须非负，limit必须为1至50")
			}
			*p.to = n
		}
	}
	return offset, limit, nil
}
func (s *Server) questionStats(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	t := s.accessibleTemplate(w, r, r.PathValue("id"), u.ID, false)
	if t == nil {
		return
	}
	offset, limit, err := libraryPage(r)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if len(r.URL.Query()["versionId"]) > 1 {
		fail(w, 400, "版本参数只能提供一次")
		return
	}
	versionID := r.URL.Query().Get("versionId")
	if versionID != "" {
		v, err := s.library.QuestionVersion(r.Context(), versionID)
		if err != nil {
			s.dbError(w, err)
			return
		}
		if v.QuestionID != t.ID {
			fail(w, 404, "版本不属于当前题目")
			return
		}
	}
	data, err := s.library.QuestionStats(r.Context(), t.ID, u.ID, versionID, offset, limit)
	if err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 200, data)
}

func (s *Server) listBanks(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	banks, err := s.library.ListQuestionBanks(r.Context(), u.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 200, banks)
}
func (s *Server) ownedBank(w http.ResponseWriter, r *http.Request, userID string) *domain.QuestionBank {
	b, err := s.library.QuestionBank(r.Context(), r.PathValue("id"))
	if err != nil {
		s.dbError(w, err)
		return nil
	}
	if b.OwnerID != userID {
		fail(w, 404, "题库不存在")
		return nil
	}
	return b
}
func (s *Server) getBank(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	if b := s.ownedBank(w, r, u.ID); b != nil {
		write(w, 200, b)
	}
}
func (s *Server) bankVersions(w http.ResponseWriter, r *http.Request, b, old *domain.QuestionBank, userID string) bool {
	retained := map[string]bool{}
	if old != nil {
		for _, id := range old.VersionIDs {
			retained[id] = true
		}
	}
	for _, id := range b.VersionIDs {
		if retained[id] {
			continue
		}
		v, err := s.library.QuestionVersion(r.Context(), id)
		if err != nil {
			s.dbError(w, err)
			return false
		}
		if s.accessibleTemplate(w, r, v.QuestionID, userID, false) == nil {
			return false
		}
	}
	return true
}
func (s *Server) createBank(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	var b domain.QuestionBank
	if !decode(w, r, &b) {
		return
	}
	b.ID = newID()
	b.OwnerID = u.ID
	b.Revision = 1
	b.CreatedAt = time.Now().UTC()
	if b.VersionIDs == nil {
		b.VersionIDs = []string{}
	}
	if err := domain.ValidateBank(&b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !s.bankVersions(w, r, &b, nil, u.ID) {
		return
	}
	if err := s.library.CreateQuestionBank(r.Context(), &b); err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 201, b)
}
func (s *Server) updateBank(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	old := s.ownedBank(w, r, u.ID)
	if old == nil {
		return
	}
	var b domain.QuestionBank
	if !decode(w, r, &b) {
		return
	}
	expected := b.Revision
	if expected < 1 {
		fail(w, 400, "更新题库需要当前revision")
		return
	}
	b.ID = old.ID
	b.OwnerID = old.OwnerID
	b.CreatedAt = old.CreatedAt
	if b.VersionIDs == nil {
		b.VersionIDs = []string{}
	}
	if err := domain.ValidateBank(&b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !s.bankVersions(w, r, &b, old, u.ID) {
		return
	}
	if err := s.library.UpdateQuestionBank(r.Context(), &b, expected); err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 200, b)
}
func (s *Server) deleteBank(w http.ResponseWriter, r *http.Request) {
	u := s.libraryUser(w, r)
	if u == nil {
		return
	}
	b := s.ownedBank(w, r, u.ID)
	if b == nil {
		return
	}
	revision, err := strconv.Atoi(r.URL.Query().Get("revision"))
	if err != nil || revision < 1 {
		fail(w, 400, "删除题库需要当前revision")
		return
	}
	if err := s.library.DeleteQuestionBank(r.Context(), b.ID, u.ID, revision); err != nil {
		s.dbError(w, err)
		return
	}
	write(w, 200, map[string]bool{"ok": true})
}

func (s *Server) validateQuestionSources(w http.ResponseWriter, r *http.Request, survey, old *domain.Survey, userID string) bool {
	previous := map[string]domain.Question{}
	if old != nil {
		for _, q := range old.Questions {
			previous[q.ID] = q
		}
	}
	for _, q := range survey.Questions {
		if q.SourceQuestionID == "" && q.SourceVersionID == "" {
			continue
		}
		if q.SourceQuestionID == "" || q.SourceVersionID == "" {
			fail(w, 400, "题目来源与版本必须同时提供")
			return false
		}
		if prior, ok := previous[q.ID]; ok && prior.SourceQuestionID == q.SourceQuestionID && prior.SourceVersionID == q.SourceVersionID && domain.QuestionContentEqual(prior, q) {
			continue
		}
		if s.library == nil {
			fail(w, 500, "题库服务未配置")
			return false
		}
		v, err := s.library.QuestionVersion(r.Context(), q.SourceVersionID)
		if err != nil {
			s.dbError(w, err)
			return false
		}
		if v.QuestionID != q.SourceQuestionID || !domain.QuestionContentEqual(v.Question, q) {
			fail(w, 400, "来源题目快照不匹配；修改内容请创建新版本或转为独立题目")
			return false
		}
		if s.accessibleTemplate(w, r, v.QuestionID, userID, false) == nil {
			return false
		}
	}
	return true
}
