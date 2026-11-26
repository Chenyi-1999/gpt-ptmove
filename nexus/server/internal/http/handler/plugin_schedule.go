package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type pluginSchedule struct {
	PluginID        string `gorm:"primaryKey;size:40" json:"-"`
	Enabled         bool   `json:"enabled"`
	Action          string `gorm:"size:40" json:"action"`
	IntervalMinutes int    `json:"interval_minutes"`
	NextAt          int64  `json:"next_at"`
	LastRunAt       int64  `json:"last_run_at"`
	LastStatus      string `gorm:"size:20" json:"last_status"`
	Revision        int64  `json:"-"`
}

func (pluginSchedule) TableName() string { return "starpt_plugin_schedules" }

func (h *pluginHandler) startSchedules(ctx context.Context) error {
	if err := h.db.AutoMigrate(&pluginSchedule{}); err != nil {
		return err
	}
	// A process interrupted after claiming a run preserves the next due time.
	if err := h.db.Model(&pluginSchedule{}).Where("last_status = ?", "running").Update("last_status", "interrupted").Error; err != nil {
		return err
	}
	go h.scheduleLoop(ctx)
	return nil
}

func (h *pluginHandler) wakeSchedules() {
	select {
	case h.schedulesChanged <- struct{}{}:
	default:
	}
}

func (h *pluginHandler) getSchedule(c *gin.Context) {
	id := c.Param("id")
	info, err := h.packages.Get(id)
	if err != nil {
		pluginError(c, err)
		return
	}
	job := pluginSchedule{PluginID: id}
	err = h.db.WithContext(c.Request.Context()).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		job.IntervalMinutes = 60
		if len(info.Actions) > 0 {
			job.Action = info.Actions[0].ID
		}
	} else if err != nil {
		pluginError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedule": job})
}

func (h *pluginHandler) putSchedule(c *gin.Context) {
	id := c.Param("id")
	info, err := h.packages.Get(id)
	if err != nil {
		pluginError(c, err)
		return
	}
	var input struct {
		Enabled         *bool  `json:"enabled"`
		Action          string `json:"action"`
		IntervalMinutes int    `json:"interval_minutes"`
	}
	if !bindPluginSettings(c, &input) {
		return
	}
	validAction := false
	for _, action := range info.Actions {
		if action.ID == input.Action {
			validAction = true
			break
		}
	}
	if input.Enabled == nil || input.IntervalMinutes < 1 || input.IntervalMinutes > 10080 ||
		!validAction {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择插件操作和 1 至 10080 分钟的运行间隔"})
		return
	}
	now := time.Now()
	job := pluginSchedule{
		PluginID: id, Enabled: *input.Enabled, Action: input.Action, IntervalMinutes: input.IntervalMinutes,
		Revision: now.UnixNano(),
	}
	if job.Enabled {
		job.NextAt = now.Add(time.Duration(job.IntervalMinutes) * time.Minute).Unix()
	}
	db := h.db.WithContext(c.Request.Context())
	err = db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "plugin_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled", "action", "interval_minutes", "next_at", "revision"}),
	}).Create(&job).Error
	if err != nil {
		pluginError(c, err)
		return
	}
	h.wakeSchedules()
	h.getSchedule(c)
}

func (h *pluginHandler) scheduleLoop(ctx context.Context) {
	for ctx.Err() == nil {
		when := h.runDueSchedule(ctx, time.Now())
		var timer *time.Timer
		var due <-chan time.Time
		if !when.IsZero() {
			timer = time.NewTimer(time.Until(when))
			due = timer.C
		}
		select {
		case <-due:
		case <-h.schedulesChanged:
		case <-ctx.Done():
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

// runDueSchedule only looks at the in-memory plugin catalog. It does not refresh it.
func (h *pluginHandler) runDueSchedule(ctx context.Context, now time.Time) time.Time {
	var enabled []string
	for _, info := range h.packages.List().Items {
		if info.Enabled {
			enabled = append(enabled, info.ID)
		}
	}
	if len(enabled) == 0 {
		return time.Time{}
	}
	db := h.db.WithContext(ctx)
	var job pluginSchedule
	err := db.Where("enabled = ? AND plugin_id IN ? AND next_at > 0", true, enabled).Order("next_at").Take(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}
	}
	if err != nil {
		return now.Add(time.Minute)
	}
	if job.NextAt > now.Unix() {
		return time.Unix(job.NextAt, 0)
	}
	interval := job.IntervalMinutes
	if interval < 1 || interval > 10080 {
		interval = 60
	}
	claim := db.Model(&pluginSchedule{}).
		Where("plugin_id = ? AND revision = ? AND next_at = ? AND enabled = ?", job.PluginID, job.Revision, job.NextAt, true).
		Updates(map[string]any{
			"next_at":     now.Add(time.Duration(interval) * time.Minute).Unix(),
			"last_run_at": now.Unix(), "last_status": "running",
		})
	if claim.Error != nil {
		return now.Add(time.Minute)
	}
	if claim.RowsAffected == 1 {
		_, err := h.invokeAction(ctx, job.PluginID, job.Action)
		status := "succeeded"
		if err != nil {
			status = "failed"
		}
		_ = db.Model(&pluginSchedule{}).
			Where("plugin_id = ? AND last_run_at = ? AND last_status = ?", job.PluginID, now.Unix(), "running").
			Update("last_status", status).Error
	}
	return now
}
