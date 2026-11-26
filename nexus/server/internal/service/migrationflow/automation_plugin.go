package migrationflow

import (
	"context"

	"github.com/pt-nexus/server/internal/repository"
)

// PluginEnabled reports the persisted master switch, independently of the transfer rules.
func (s *AutomationService) PluginEnabled(ctx context.Context) (bool, error) {
	return s.repo.WithContext(ctx).PluginEnabled()
}

// SetPluginEnabled changes only the master switch. Existing jobs, rules and evidence are kept.
func (s *AutomationService) SetPluginEnabled(ctx context.Context, enabled bool) error {
	return s.repo.WithContext(ctx).SetPluginEnabled(enabled)
}

func (s *AutomationService) requirePluginEnabled(ctx context.Context) error {
	enabled, err := s.PluginEnabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return repository.ErrAutomationPluginDisabled
	}
	return nil
}
