package web

import (
	"net/http"
	"strings"

	"github.com/tylersriver/mend/internal/web/view"
)

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func (s *Server) taskList(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.ListTasks(r.Context())
	if err != nil {
		s.fail(w, "list tasks", err)
		return
	}
	s.render(w, r, view.TasksPage(s.profile(r.Context()), tasks))
}

func (s *Server) taskCreate(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	t, err := s.store.AddTask(r.Context(), title)
	if err != nil {
		s.fail(w, "add task", err)
		return
	}
	if isHTMX(r) {
		s.render(w, r, view.TaskRow(t))
		return
	}
	http.Redirect(w, r, "/tasks", http.StatusSeeOther)
}

func (s *Server) taskToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	t, err := s.store.ToggleTask(r.Context(), id)
	if err != nil {
		s.fail(w, "toggle task", err)
		return
	}
	if isHTMX(r) {
		s.render(w, r, view.TaskRow(t))
		return
	}
	http.Redirect(w, r, "/tasks", http.StatusSeeOther)
}

func (s *Server) taskDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteTask(r.Context(), id); err != nil {
		s.fail(w, "delete task", err)
		return
	}
	if isHTMX(r) {
		w.WriteHeader(http.StatusOK) // empty body → htmx removes the row
		return
	}
	http.Redirect(w, r, "/tasks", http.StatusSeeOther)
}
