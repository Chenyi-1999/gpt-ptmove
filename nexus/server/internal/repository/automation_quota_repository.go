package repository

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	automationPrePublishStates = []string{"reserved", "downloading", "preparing", AutomationStateWaitingMedia, AutomationStateWaitingTarget, AutomationStateWaitingBatch, "ready", "queued_publish"}
	automationPublishedStates  = []string{"published", "seeding", "succeeded"}
	automationUncertainStates  = []string{"publishing", "needs_review", "reconciling"}
)

const automationPublishMonthExpr = "COALESCE(NULLIF(publish_month, ''), month)"
const automationSuccessExpr = "(state IN ? OR (COALESCE(publish_confirmed_at, 0) > 0 AND state <> ?))"

// automationConfirmTime 保存最早的确认时间，避免后续做种核对覆盖发布回执。
func automationConfirmTime(now time.Time) clause.Expr {
	return gorm.Expr("COALESCE(NULLIF(publish_confirmed_at, 0), ?)", now.UnixMilli())
}

// automationShanghaiMonth 以统一的上海月份记账，不依赖宿主机时区。
func automationShanghaiMonth(now time.Time) string {
	return now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01")
}

// lockAutomationQuotaMonth 串行化该月的新下载预留与发布尝试跨月占位。
func lockAutomationQuotaMonth(tx *gorm.DB, month string) error {
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&AutomationQuota{Month: month}).Error; err != nil {
		return err
	}
	return tx.Model(&AutomationQuota{}).Where("month = ?", month).
		UpdateColumn("count", gorm.Expr("count")).Error
}

// automationPublishSlots 统计当月已发布和无法排除成功的尝试，历史记录沿用原登记月。
func automationPublishSlots(db *gorm.DB, month string) *gorm.DB {
	return db.Model(&AutomationTask{}).Where(automationPublishMonthExpr+" = ?", month).
		Where("("+automationSuccessExpr+" OR (publish_attempted = ? AND state IN ?))", automationPublishedStates, AutomationStateAlreadyExists, true, automationUncertainStates)
}

// automationMonthUsage 在一个数据库快照里读取已发种与保守占位，排除已确认未发布的失败和重复。
func automationMonthUsage(db *gorm.DB, month string) (published, pending int64, err error) {
	var counts struct{ Published, Pending int64 }
	err = db.Model(&AutomationTask{}).Select(`
		COALESCE(SUM(CASE WHEN `+automationPublishMonthExpr+` = ? AND `+automationSuccessExpr+` THEN 1 ELSE 0 END), 0) AS published,
		COALESCE(SUM(CASE WHEN
			(month = ? AND state IN ?)
			OR (`+automationPublishMonthExpr+` = ? AND publish_attempted = ? AND state IN ? AND COALESCE(publish_confirmed_at, 0) = 0)
		THEN 1 ELSE 0 END), 0) AS pending`,
		month, automationPublishedStates, AutomationStateAlreadyExists, month, automationPrePublishStates, month, true, automationUncertainStates).
		Where("month = ? OR publish_month = ?", month, month).Scan(&counts).Error
	return counts.Published, counts.Pending, err
}
