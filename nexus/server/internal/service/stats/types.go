package stats

import (
	"time"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
)

type Service struct {
	repo               *repository.StatsRepository
	cfg                *config.Manager
	recentSpeedSamples func(start, end time.Time) []repository.SpeedRow
}

// New 创建统计服务，允许将下载器近期采样与数据库统计合并。
func New(repo *repository.StatsRepository, cfg *config.Manager, recentSpeedSamples func(start, end time.Time) []repository.SpeedRow) *Service {
	return &Service{repo: repo, cfg: cfg, recentSpeedSamples: recentSpeedSamples}
}
