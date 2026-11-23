package migrationflow

import (
	"sync"
	"time"
)

const (
	observationRetention     = 30 * time.Minute
	maxCompletedObservations = 60
)

type taskObservations struct {
	mu   sync.Mutex
	rows map[string]observedTask
}

type observedTask struct {
	kind, title, route        string
	status, message, progress string
	outcome                   string
	started, updated          time.Time
}

type taskObservation struct {
	state *taskObservations
	id    string
}

// observeTask 登记独立的用户操作，便于各页面读取相同的任务快照。
func (s *MigrateService) observeTask(kind, title, route string) taskObservation {
	if s == nil || s.observations == nil {
		return taskObservation{}
	}
	now := time.Now()
	id := s.newID("operation")
	state := s.observations
	state.mu.Lock()
	state.prune(now)
	state.rows[id] = observedTask{kind: kind, title: title, route: route, status: "running", message: "正在准备", started: now, updated: now}
	state.mu.Unlock()
	return taskObservation{state: state, id: id}
}

// update 只记录实际改变的阶段或计数，已完成任务忽略迟到更新。
func (o taskObservation) update(message, progress string) {
	o.write("running", message, progress, "")
}

// finish 保留终态，确保短任务也能出现在下一次刷新中。
func (o taskObservation) finish(success bool, message, progress string) {
	o.finishWithOutcome(success, message, progress, "")
}

func (o taskObservation) finishWithOutcome(success bool, message, progress, outcome string) {
	status := "failed"
	if success {
		status = "success"
	}
	o.write(status, message, progress, outcome)
}

func (o taskObservation) write(status, message, progress, outcome string) {
	if o.state == nil {
		return
	}
	o.state.mu.Lock()
	defer o.state.mu.Unlock()
	task, ok := o.state.rows[o.id]
	if !ok || task.status != "running" || (task.status == status && task.message == message && task.progress == progress) {
		return
	}
	task.status, task.message, task.progress, task.updated = status, message, progress, time.Now()
	task.outcome = outcome
	o.state.rows[o.id] = task
	if status != "running" {
		o.state.prune(task.updated)
	}
}

// prune 只回收终态旧记录，运行中的观察独立保留。
func (s *taskObservations) prune(now time.Time) {
	completed := 0
	for id, task := range s.rows {
		if task.status == "running" {
			continue
		}
		if now.Sub(task.updated) > observationRetention {
			delete(s.rows, id)
		} else {
			completed++
		}
	}
	for completed > maxCompletedObservations {
		oldest := ""
		for id, task := range s.rows {
			if task.status != "running" && (oldest == "" || task.updated.Before(s.rows[oldest].updated)) {
				oldest = id
			}
		}
		if oldest == "" {
			return
		}
		delete(s.rows, oldest)
		completed--
	}
}

// snapshot 读取不含凭据、日志或文件路径的任务显示字段。
func (s *taskObservations) snapshot() []map[string]any {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(time.Now())
	rows := make([]map[string]any, 0, len(s.rows))
	for id, task := range s.rows {
		row := map[string]any{
			"key": "operation:" + id, "kind": task.kind, "title": task.title, "status": task.status,
			"message": task.message, "progress_text": task.progress,
			"started_at": task.started.UnixMilli(), "updated_at": task.updated.UnixMilli(),
		}
		if task.route != "" {
			row["route_target"] = map[string]string{"path": task.route}
		}
		if task.outcome != "" {
			row["outcome"] = task.outcome
		}
		rows = append(rows, row)
	}
	return rows
}
