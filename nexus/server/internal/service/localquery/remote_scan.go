package localquery

import (
	"context"
	"errors"
	"maps"
	"path"
	"sort"
	"strings"

	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

var errScanIncomplete = errors.New("扫描未完成，请缩小范围后重试")

type remoteScanClient interface {
	ListRemoteScanReferences(context.Context) ([]downloaderclient.RemoteScanReference, error)
	ScanMediaDirectoryByProxy(context.Context, downloaderclient.RemoteDirectoryScanRequest) (downloaderclient.RemoteDirectoryScanResult, error)
}

type remoteScopeKey struct {
	peer      string
	directory string
}

type remoteScanPeers struct {
	open      func(map[string]any, string) (remoteScanClient, error)
	cfg       map[string]any
	members   map[string][]string
	snapshots map[string]remotePeerSnapshot
}

type remotePeerSnapshot struct {
	clients   map[string]remoteScanClient
	inventory map[string]map[string]downloaderclient.RemoteScanReference
	err       error
}

func newRemoteScanClient(root map[string]any, id string) (remoteScanClient, error) {
	return downloaderclient.FromConfig(root, id)
}

func newRemoteScanPeers(open func(map[string]any, string) (remoteScanClient, error), cfg map[string]any, downloaders map[string]downloaderMeta) *remoteScanPeers {
	peers := &remoteScanPeers{open: open, cfg: cfg, members: map[string][]string{}, snapshots: map[string]remotePeerSnapshot{}}
	for _, meta := range downloaders {
		if meta.Remote && meta.ScanPeer != "" {
			peers.members[meta.ScanPeer] = append(peers.members[meta.ScanPeer], meta.ID)
		}
	}
	for _, members := range peers.members {
		sort.Strings(members)
	}
	return peers
}

// snapshot 读取同一媒体端口下各 qB 的完整清单，每次扫描只读取一次。
func (p *remoteScanPeers) snapshot(ctx context.Context, peer string) remotePeerSnapshot {
	if snapshot, ok := p.snapshots[peer]; ok {
		return snapshot
	}
	return p.readSnapshot(ctx, peer)
}

func (p *remoteScanPeers) readSnapshot(ctx context.Context, peer string) remotePeerSnapshot {
	snapshot := remotePeerSnapshot{clients: map[string]remoteScanClient{}, inventory: map[string]map[string]downloaderclient.RemoteScanReference{}}
	for _, id := range p.members[peer] {
		if ctx.Err() != nil {
			snapshot.err = errScanIncomplete
			break
		}
		client, err := p.open(p.cfg, id)
		if err != nil {
			snapshot.err = errors.New("远程下载器连接配置不可用")
			break
		}
		refs, err := client.ListRemoteScanReferences(ctx)
		if err != nil {
			if ctx.Err() != nil {
				err = errScanIncomplete
			}
			snapshot.err = err
			break
		}
		byHash := make(map[string]downloaderclient.RemoteScanReference, len(refs))
		for _, ref := range refs {
			byHash[strings.ToLower(ref.Hash)] = ref
		}
		snapshot.inventory[id] = byHash
		snapshot.clients[id] = client
	}
	p.snapshots[peer] = snapshot
	return snapshot
}

// scan 根据完整 qB 清单生成顶层引用和本次需要核验的项目，客户端只回传差异。
func (p *remoteScanPeers) scan(ctx context.Context, scope remoteScopeKey, rows []scanTorrent) (directoryScan, []string, error) {
	if ctx.Err() != nil {
		return directoryScan{}, nil, errScanIncomplete
	}
	directory := scope.directory
	if !path.IsAbs(directory) || directory == "/" || path.Clean(directory) != directory {
		return directoryScan{}, nil, errors.New("远程目录路径不受支持")
	}
	snapshot := p.snapshot(ctx, scope.peer)
	if snapshot.err != nil {
		return directoryScan{}, nil, snapshot.err
	}
	client := snapshot.clients[rows[0].downloader.ID]
	if client == nil {
		return directoryScan{}, nil, errors.New("远程下载器连接配置不可用")
	}
	referenced := map[string]bool{}
	for _, tasks := range snapshot.inventory {
		for _, task := range tasks {
			// 无独立内容目录的多文件任务，单凭 content_path 无法区分文件归属。
			if task.ContentPath == directory || strings.HasPrefix(directory, strings.TrimRight(task.ContentPath, "/")+"/") {
				return directoryScan{}, nil, errors.New("有任务直接引用该目录或其上级，无法完整判定孤立文件")
			}
			if name := remoteTopLevelName(directory, task.ContentPath); name != "" {
				referenced[name] = true
			} else if task.SavePath == directory || strings.HasPrefix(task.SavePath, directory+"/") {
				return directoryScan{}, nil, errors.New("目录中有任务的保存路径和内容路径不一致，无法完整判定孤立文件")
			}
		}
	}
	expected := make([]string, 0, len(rows))
	requested := map[string]bool{}
	for _, row := range rows {
		task, present := snapshot.inventory[row.downloader.ID][strings.ToLower(strings.TrimSpace(row.record.Hash))]
		name := remoteTopLevelName(directory, task.ContentPath)
		if !present || name == "" {
			return directoryScan{}, nil, errors.New("下载器中的任务或保存目录已变化，请刷新数据后重试")
		}
		expected = append(expected, name)
		requested[name] = true
	}
	observed, err := client.ScanMediaDirectoryByProxy(ctx, downloaderclient.RemoteDirectoryScanRequest{
		RemotePath: directory, Expected: sortedKeys(requested), Referenced: sortedKeys(referenced),
	})
	if err != nil {
		if ctx.Err() != nil {
			return directoryScan{}, nil, errScanIncomplete
		}
		return directoryScan{}, nil, err
	}
	if len(observed.Orphaned) > 0 {
		// 下载器可以引用目录中已有的文件；即使目录没有变化，也要复核孤立结果。
		latest := p.readSnapshot(ctx, scope.peer)
		if latest.err != nil {
			return directoryScan{}, nil, latest.err
		}
		if !maps.EqualFunc(snapshot.inventory, latest.inventory, func(a, b map[string]downloaderclient.RemoteScanReference) bool {
			return maps.Equal(a, b)
		}) {
			return directoryScan{}, nil, errors.New("扫描期间下载器引用发生变化，请重新扫描")
		}
	}
	result := directoryScan{items: observed.TotalItems, missing: map[string]bool{}}
	for _, name := range observed.Missing {
		result.missing[name] = true
	}
	for _, file := range observed.Orphaned {
		result.orphaned = append(result.orphaned, map[string]any{
			"name": file.Name, "path": directory, "full_path": path.Join(directory, file.Name), "size": file.Size,
			"downloader_name": rows[0].downloader.Name,
		})
	}
	return result, expected, nil
}

func remoteTopLevelName(root, filename string) string {
	relative, ok := strings.CutPrefix(filename, root+"/")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(relative, "/")
	return name
}
