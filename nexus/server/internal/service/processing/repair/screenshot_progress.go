package repair

import "sync"

// reportScreenshotProgress 仅报告处理流程实际观察到的进度；负数表示代理未提供细分进度。
func (input ScreenshotGenerateInput) reportScreenshotProgress(stage string, completed, total int) {
	if input.Progress != nil {
		input.Progress(stage, completed, total)
	}
}

// screenshotProgressCounter 协调串行截帧和并行上传的计数。截帧完成后再展示上传阶段。
type screenshotProgressCounter struct {
	input        ScreenshotGenerateInput
	mu           sync.Mutex
	captureTotal int
	captured     int
	uploadTotal  int
	uploaded     int
}

func startScreenshotProgress(input ScreenshotGenerateInput, total int) *screenshotProgressCounter {
	input.reportScreenshotProgress("capturing", 0, total)
	return &screenshotProgressCounter{input: input, captureTotal: total}
}

// frameCaptured 只在读到有效的帧文件后调用。
func (counter *screenshotProgressCounter) frameCaptured() {
	counter.mu.Lock()
	defer counter.mu.Unlock()
	counter.captured++
	counter.input.reportScreenshotProgress("capturing", counter.captured, counter.captureTotal)
}

// startUploads 发布已排入上传队列的总数，计入同时完成的上传。
func (counter *screenshotProgressCounter) startUploads(total int) {
	if total == 0 {
		return
	}
	counter.mu.Lock()
	defer counter.mu.Unlock()
	counter.uploadTotal = total
	counter.input.reportScreenshotProgress("uploading", counter.uploaded, counter.uploadTotal)
}

// frameUploaded 记录一次图床确认，更新时保持计数单调。
func (counter *screenshotProgressCounter) frameUploaded() {
	counter.mu.Lock()
	defer counter.mu.Unlock()
	counter.uploaded++
	if counter.uploadTotal > 0 {
		counter.input.reportScreenshotProgress("uploading", counter.uploaded, counter.uploadTotal)
	}
}

// reportRemoteScreenshotResult 用现有 HTTPS 协议返回的可用帧数更新最终数量。
func (input ScreenshotGenerateInput) reportRemoteScreenshotResult(stage string, actual, requested int) {
	input.reportScreenshotProgress(stage, actual, max(actual, requested))
}
