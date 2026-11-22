package service

import (
	"time"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	statssvc "github.com/pt-nexus/server/internal/service/stats"
)

type StatsService = statssvc.Service

// NewStatsService 创建统计服务并接入下载器速率的内存快照。
func NewStatsService(repo *repository.StatsRepository, cfg *config.Manager, recentSpeedSamples func(start, end time.Time) []repository.SpeedRow) *StatsService {
	return statssvc.New(repo, cfg, recentSpeedSamples)
}
