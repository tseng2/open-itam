// Package updater 实现 Agent 自更新：下载 → sha256 校验 → 生成替换脚本 →
// 退出自身由脚本完成二进制替换并拉起服务。Windows 无法替换运行中的 exe，
// 所以替换动作必须由独立进程在 Agent 退出后执行
package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"itagent/internal/agent/reporter"
	"itagent/internal/shared/protocol"
)

// Apply 执行更新流程，成功返回后调用方应立即退出进程。
// 全路径写 update.log（下载失败/校验失败同样留痕）——2026-09-26 排查
// .160 终端自更新离线案时发现下载与校验失败只有进程内日志，现场无从考证
func Apply(u *reporter.Uploader, info *protocol.UpdateInfo, installDir string) error {
	updateDir := filepath.Join(installDir, "data", "update")
	if err := os.MkdirAll(updateDir, 0o755); err != nil {
		return err
	}
	logf := func(format string, args ...any) {
		f, err := os.OpenFile(filepath.Join(updateDir, "update.log"),
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, time.Now().UTC().Format(time.RFC3339)+" "+format+"\n", args...)
	}
	newExe := filepath.Join(updateDir, "core-agent.new.exe")

	// 已下载且校验通过的包直接复用：上次可能是脚本执行阶段失败
	if !verifyFile(newExe, info.SHA256) {
		if err := u.DownloadTo("/api/v1/agent/update/download", newExe); err != nil {
			logf("download update v%s failed: %v", info.Version, err)
			return fmt.Errorf("download update: %w", err)
		}
		if !verifyFile(newExe, info.SHA256) {
			os.Remove(newExe)
			logf("update package v%s sha256 mismatch", info.Version)
			return fmt.Errorf("update package sha256 mismatch")
		}
	}

	// 由新包自己完成替换：解释器在服务进程里常被终端安全软件拦截，
	// 换成自研二进制做 SCM 停启 + 文件替换，整条链不依赖外部解释器
	if err := spawnApply(newExe, installDir, os.Getpid()); err != nil {
		logf("spawn apply failed: %v", err)
		return fmt.Errorf("spawn apply: %w", err)
	}
	logf("updater spawned for v%s, agent exiting", info.Version)
	return nil
}

func verifyFile(path, wantSHA256 string) bool {
	if wantSHA256 == "" {
		// 清单未配摘要时无法校验，宁可不更新
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == wantSHA256
}

// applyScript 生成替换脚本：等待 Agent 退出 → 备份旧版 → 替换 → 起服务 →
// 服务起不来则回滚旧版
func applyScript(installDir, newExe string) string {
	target := filepath.Join(installDir, "bin", "core-agent.exe")
	backup := target + ".bak"
	return fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
for ($i = 0; $i -lt 60; $i++) {
    if (-not (Get-Process core-agent -ErrorAction SilentlyContinue)) { break }
    Start-Sleep -Seconds 1
}
Get-Process agent-watchdog -ErrorAction SilentlyContinue | Stop-Process -Force
sc.exe stop ITAgentService 2>$null | Out-Null
Start-Sleep -Seconds 2
try {
    Copy-Item '%[2]s' '%[3]s' -Force
    Copy-Item '%[1]s' '%[2]s' -Force
    Start-Service ITAgentService
    Start-Sleep -Seconds 8
    if ((Get-Service ITAgentService).Status -ne 'Running') { throw 'service not running after update' }
} catch {
    Copy-Item '%[3]s' '%[2]s' -Force
    Start-Service ITAgentService
}
Remove-Item '%[1]s' -ErrorAction SilentlyContinue
Remove-Item $MyInvocation.MyCommand.Path -ErrorAction SilentlyContinue
`, newExe, target, backup)
}
