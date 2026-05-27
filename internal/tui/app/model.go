package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	controlplanev1 "mini-cloud/internal/gen/proto/minicloud/controlplane/v1"

	tea "github.com/charmbracelet/bubbletea"
)

type OperatorClient interface {
	GetOverview(context.Context) (*controlplanev1.Overview, error)
	ListPlanes(context.Context) ([]*controlplanev1.Plane, error)
	SyncPlane(context.Context, string) (*controlplanev1.SyncPlaneResponse, error)
	ListProjects(context.Context) ([]*controlplanev1.Project, error)
	ListServices(context.Context, string) ([]*controlplanev1.Service, error)
	CreateService(context.Context, *controlplanev1.CreateServiceRequest) (*controlplanev1.Service, error)
}

type Tab int

const (
	TabOverview Tab = iota
	TabPlanes
	TabProjects
	TabServices
)

var tabLabels = []string{"Overview", "Planes", "Projects", "Services"}

type Options struct {
	InitialProjectID string
}

type Model struct {
	client OperatorClient

	activeTab Tab
	width     int
	height    int
	loading   bool
	err       string
	flash     string

	overview          *controlplanev1.Overview
	planes            []*controlplanev1.Plane
	projects          []*controlplanev1.Project
	services          []*controlplanev1.Service
	selectedPlane     int
	selectedProject   int
	selectedService   int
	activeProjectID   string
	activeProjectName string
	createForm        *serviceCreateForm
}

type refreshMsg struct {
	overview          *controlplanev1.Overview
	planes            []*controlplanev1.Plane
	projects          []*controlplanev1.Project
	services          []*controlplanev1.Service
	activeProjectID   string
	activeProjectName string
	flash             string
	err               error
}

type actionMsg struct {
	service           *controlplanev1.Service
	flash             string
	selectedServiceID string
	err               error
}

func New(client OperatorClient, opts Options) Model {
	return Model{
		client:          client,
		activeTab:       TabOverview,
		loading:         true,
		activeProjectID: strings.TrimSpace(opts.InitialProjectID),
	}
}

func (m Model) Init() tea.Cmd {
	return m.refreshCmd(m.activeProjectID)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		return m, nil
	case tea.KeyMsg:
		if m.createForm != nil {
			return m.updateCreateFormKey(typed)
		}
		return m.updateKey(typed)
	case refreshMsg:
		m.loading = false
		if typed.err != nil {
			m.err = typed.err.Error()
			return m, nil
		}
		m.err = ""
		m.overview = typed.overview
		m.planes = typed.planes
		m.projects = typed.projects
		m.services = typed.services
		m.activeProjectID = typed.activeProjectID
		m.activeProjectName = typed.activeProjectName
		m.flash = typed.flash
		m.selectedPlane = clampIndex(m.selectedPlane, len(m.planes))
		m.selectedProject = clampProjectSelection(m.selectedProject, m.projects, m.activeProjectID)
		m.selectedService = clampIndex(m.selectedService, len(m.services))
		return m, nil
	case actionMsg:
		m.loading = false
		if typed.err != nil {
			m.err = typed.err.Error()
			return m, nil
		}
		m.err = ""
		m.flash = typed.flash
		m.services = upsertService(m.services, typed.service)
		if idx := findServiceIndex(m.services, typed.selectedServiceID); idx >= 0 {
			m.selectedService = idx
		} else {
			m.selectedService = clampIndex(m.selectedService, len(m.services))
		}
		m.createForm = nil
		return m, nil
	}
	return m, nil
}

func (m Model) View() string {
	var out strings.Builder
	out.WriteString(renderTabs(m.activeTab))
	out.WriteString("\n")
	if m.loading {
		out.WriteString("loading...\n")
		return out.String()
	}
	if m.err != "" {
		out.WriteString("error: " + m.err + "\n")
	}
	if m.flash != "" {
		out.WriteString("info: " + m.flash + "\n")
	}
	out.WriteString("\n")
	out.WriteString(m.renderBody())
	out.WriteString("\n")
	out.WriteString(renderHelp(m.activeTab))
	return out.String()
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "tab", "right", "l":
		m.activeTab = Tab((int(m.activeTab) + 1) % len(tabLabels))
		return m, nil
	case "shift+tab", "left", "h":
		m.activeTab = Tab((int(m.activeTab) - 1 + len(tabLabels)) % len(tabLabels))
		return m, nil
	case "1":
		m.activeTab = TabOverview
		return m, nil
	case "2":
		m.activeTab = TabPlanes
		return m, nil
	case "3":
		m.activeTab = TabProjects
		return m, nil
	case "4":
		m.activeTab = TabServices
		return m, nil
	case "r":
		m.loading = true
		m.flash = ""
		m.err = ""
		return m, m.refreshCmd(m.activeProjectID)
	}

	switch m.activeTab {
	case TabPlanes:
		return m.updatePlanesKey(msg)
	case TabProjects:
		return m.updateProjectsKey(msg)
	case TabServices:
		return m.updateServicesKey(msg)
	default:
		return m, nil
	}
}

func (m Model) updatePlanesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.selectedPlane = moveUp(m.selectedPlane)
	case "down", "j":
		m.selectedPlane = moveDown(m.selectedPlane, len(m.planes))
	case "s":
		selected := m.currentPlane()
		if selected == nil {
			return m, nil
		}
		m.loading = true
		m.flash = ""
		m.err = ""
		return m, m.syncPlaneCmd(selected.GetId(), selected.GetDisplayName())
	}
	return m, nil
}

func (m Model) updateProjectsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.selectedProject = moveUp(m.selectedProject)
	case "down", "j":
		m.selectedProject = moveDown(m.selectedProject, len(m.projects))
	case "enter":
		selected := m.currentProject()
		if selected == nil {
			return m, nil
		}
		m.loading = true
		m.flash = ""
		m.err = ""
		return m, m.refreshCmd(selected.GetId())
	}
	return m, nil
}

func (m Model) updateServicesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.selectedService = moveUp(m.selectedService)
	case "down", "j":
		m.selectedService = moveDown(m.selectedService, len(m.services))
	case "n":
		if strings.TrimSpace(m.activeProjectID) == "" {
			m.err = "select a project before creating a service"
			return m, nil
		}
		m.createForm = newServiceCreateForm(m.planes)
		m.err = ""
		m.flash = ""
		return m, nil
	}
	return m, nil
}

func (m Model) updateCreateFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.createForm = nil
		m.flash = "canceled create-service form"
		m.err = ""
		return m, nil
	case "up", "k":
		m.createForm.moveUp()
		return m, nil
	case "down", "j", "tab":
		m.createForm.moveDown()
		return m, nil
	case "shift+tab":
		m.createForm.moveUp()
		return m, nil
	case "backspace":
		m.createForm.backspace()
		return m, nil
	case "ctrl+u":
		m.createForm.clear()
		return m, nil
	case "enter":
		req, err := m.createForm.request(m.activeProjectID)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.loading = true
		m.err = ""
		m.flash = ""
		return m, m.createServiceCmd(req)
	}

	if len(msg.Runes) > 0 {
		m.createForm.appendRunes(msg.Runes)
		return m, nil
	}
	return m, nil
}

func (m Model) createServiceCmd(req *controlplanev1.CreateServiceRequest) tea.Cmd {
	client := m.client
	serviceName := req.GetDisplayName()
	if strings.TrimSpace(serviceName) == "" {
		serviceName = req.GetName()
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		created, err := client.CreateService(ctx, req)
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{
			service:           created,
			flash:             fmt.Sprintf("created service: %s", serviceName),
			selectedServiceID: created.GetMetadata().GetId(),
		}
	}
}

func (m Model) refreshCmd(projectID string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		overview, err := client.GetOverview(ctx)
		if err != nil {
			return refreshMsg{err: err}
		}
		planes, err := client.ListPlanes(ctx)
		if err != nil {
			return refreshMsg{err: err}
		}
		projects, err := client.ListProjects(ctx)
		if err != nil {
			return refreshMsg{err: err}
		}

		activeProjectID, activeProjectName := selectProject(projects, projectID)
		var services []*controlplanev1.Service
		if activeProjectID != "" {
			services, err = client.ListServices(ctx, activeProjectID)
			if err != nil {
				return refreshMsg{err: err}
			}
		}

		return refreshMsg{
			overview:          overview,
			planes:            planes,
			projects:          projects,
			services:          services,
			activeProjectID:   activeProjectID,
			activeProjectName: activeProjectName,
			flash:             "",
			err:               nil,
		}
	}
}

func (m Model) syncPlaneCmd(planeID string, planeName string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_, err := client.SyncPlane(ctx, planeID)
		if err != nil {
			return refreshMsg{err: err}
		}
		refreshed := m.refreshCmd(m.activeProjectID)
		msg := refreshed()
		if payload, ok := msg.(refreshMsg); ok && payload.err == nil {
			payload.flash = fmt.Sprintf("synced plane: %s", planeName)
			return payload
		}
		return msg
	}
}

func (m Model) renderBody() string {
	switch m.activeTab {
	case TabOverview:
		return renderOverview(m.overview)
	case TabPlanes:
		return renderPlanes(m.planes, m.selectedPlane)
	case TabProjects:
		return renderProjects(m.projects, m.selectedProject, m.activeProjectID)
	case TabServices:
		return renderServices(m.activeProjectName, m.activeProjectID, m.services, m.selectedService, m.createForm)
	default:
		return ""
	}
}

func (m Model) currentPlane() *controlplanev1.Plane {
	if len(m.planes) == 0 || m.selectedPlane < 0 || m.selectedPlane >= len(m.planes) {
		return nil
	}
	return m.planes[m.selectedPlane]
}

func (m Model) currentProject() *controlplanev1.Project {
	if len(m.projects) == 0 || m.selectedProject < 0 || m.selectedProject >= len(m.projects) {
		return nil
	}
	return m.projects[m.selectedProject]
}

func moveUp(index int) int {
	if index <= 0 {
		return 0
	}
	return index - 1
}

func moveDown(index int, length int) int {
	if length == 0 {
		return 0
	}
	if index >= length-1 {
		return length - 1
	}
	return index + 1
}

func clampIndex(index int, length int) int {
	if length == 0 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index >= length {
		return length - 1
	}
	return index
}

func clampProjectSelection(index int, projects []*controlplanev1.Project, activeProjectID string) int {
	if selected := findProjectIndex(projects, activeProjectID); selected >= 0 {
		return selected
	}
	return clampIndex(index, len(projects))
}

func selectProject(projects []*controlplanev1.Project, requestedID string) (string, string) {
	if idx := findProjectIndex(projects, requestedID); idx >= 0 {
		return projects[idx].GetId(), projectDisplayName(projects[idx])
	}
	if len(projects) == 0 {
		return "", ""
	}
	return projects[0].GetId(), projectDisplayName(projects[0])
}

func findProjectIndex(projects []*controlplanev1.Project, projectID string) int {
	requested := strings.TrimSpace(projectID)
	if requested == "" {
		return -1
	}
	for idx, item := range projects {
		if item.GetId() == requested {
			return idx
		}
	}
	return -1
}

func projectDisplayName(item *controlplanev1.Project) string {
	if item == nil {
		return ""
	}
	if display := strings.TrimSpace(item.GetDisplayName()); display != "" {
		return display
	}
	return item.GetName()
}

func upsertService(items []*controlplanev1.Service, updated *controlplanev1.Service) []*controlplanev1.Service {
	if updated == nil {
		return items
	}
	out := make([]*controlplanev1.Service, 0, len(items))
	replaced := false
	for _, item := range items {
		if item.GetMetadata().GetId() == updated.GetMetadata().GetId() {
			out = append(out, updated)
			replaced = true
			continue
		}
		out = append(out, item)
	}
	if !replaced {
		out = append(out, updated)
	}
	return out
}

func findServiceIndex(items []*controlplanev1.Service, serviceID string) int {
	needle := strings.TrimSpace(serviceID)
	if needle == "" {
		return -1
	}
	for idx, item := range items {
		if item.GetMetadata().GetId() == needle {
			return idx
		}
	}
	return -1
}
