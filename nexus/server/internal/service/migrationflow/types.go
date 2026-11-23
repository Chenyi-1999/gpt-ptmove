package migrationflow

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	extract "github.com/pt-nexus/server/internal/service/acquire/extract"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	processingbdflow "github.com/pt-nexus/server/internal/service/processing/bdflow"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

type MigrateService struct {
	automation *AutomationService
	repo       *repository.MigrateRepository
	cfg        *config.Manager

	extractorEngine *extract.Engine

	contextState            *publishworkflow.ContextState
	logStreamState          *acquirefetch.LogStreamState
	batchFetchState         *acquirefetch.BatchFetchState
	publishState            *publishworkflow.BatchState
	bdinfoState             *processingbdflow.BDInfoState
	observations            *taskObservations
	maintenanceMediaRunning atomic.Bool
	maintenanceMediaWrites  sync.Mutex

	queueRepo         *repository.PublishQueueRepository
	publishLogRepo    *repository.PublishLogRepository
	publishedEditRepo *repository.PublishedEditRepository
	publishedEditKey  [32]byte
	statsRepo         *repository.StatsRepository

	queueStartOnce sync.Once
	queueStopCh    chan struct{}
	queueDoneCh    chan struct{}
}

func NewMigrateService(repo *repository.MigrateRepository, cfg *config.Manager) *MigrateService {
	return &MigrateService{
		repo:            repo,
		cfg:             cfg,
		extractorEngine: extract.NewPageExtractorEngine(),
		contextState:    publishworkflow.NewContextState(),
		logStreamState:  acquirefetch.NewLogStreamState(),
		batchFetchState: acquirefetch.NewBatchFetchState(),
		publishState:    publishworkflow.NewBatchState(),
		bdinfoState:     processingbdflow.NewBDInfoState(),
		observations:    &taskObservations{rows: make(map[string]observedTask)},
	}
}

func (s *MigrateService) newID(prefix string) string {
	return fmt.Sprintf("%s-%d-%06d", prefix, time.Now().UnixNano(), rand.Intn(1000000))
}
