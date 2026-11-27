package localquery

import (
	"path/filepath"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
)

type pathMapping struct {
	Remote string
	Local  string
}

type downloaderMeta struct {
	ID       string
	Name     string
	Mappings []pathMapping
	Remote   bool
	ScanPeer string
}

type syncedItem struct {
	Name            string
	Path            string
	Size            int64
	Count           int
	DownloaderNames map[string]struct{}
}

type Service struct {
	repo           *repository.LocalQueryRepository
	cfg            *config.Manager
	cacheFile      string
	openRemoteScan func(map[string]any, string) (remoteScanClient, error)
}

func New(repo *repository.LocalQueryRepository, cfg *config.Manager, dataDir string) *Service {
	return &Service{
		repo:           repo,
		cfg:            cfg,
		cacheFile:      filepath.Join(dataDir, "local_scan_cache.json"),
		openRemoteScan: newRemoteScanClient,
	}
}
