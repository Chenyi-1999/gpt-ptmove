package repository

import "context"

// WithContext 使机器 API 幂等事务与任务、配额、设置写入共享一次提交。
func (r *AutomationRepository) WithContext(ctx context.Context) *AutomationRepository {
	if db := MachineTransaction(ctx); db != nil {
		return &AutomationRepository{db: db.WithContext(ctx)}
	}
	return &AutomationRepository{db: r.db.WithContext(ctx)}
}
