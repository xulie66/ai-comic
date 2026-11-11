package jobs

func (m *Manager) Snapshot() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Status{}
	for _, r := range m.runs {
		out = append(out, status(r))
	}
	return out
}
