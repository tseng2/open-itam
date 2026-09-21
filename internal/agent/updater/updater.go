// Package updater 实现 Agent 自更新：下载 → sha256 校验 → 生成替换脚本 →
// 退出自身由脚本完成二进制替换并拉起服务。Windows 无法替换运行中的 exe，
// 所以替换动作必须由独立进程在 Agent 退出后执行
package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"itagent/internal/agent/reporter"
	"itagent/internal/shared/protocol"
)

// Apply 执行更新流程，成功返回后调用方应立即退出进程
func Apply(u *reporter.Uploader, info *protocol.UpdateInfo, installDir string) error {
	updateDir := filepath.Join(installDir, "data", "update")
	if err := os.MkdirAll(updateDir, 0o755); err != nil {
		return err
	}
	newExe := filepath.Join(updateDir, "core-agent.new.exe")

	// 已下载且校验通过的包直接复用：上次可能是脚本执行阶段失败
	if !verifyFile(newExe, info.SHA256) {
		if err := u.DownloadTo("/api/v1/agent/update/download", newExe); err != nil {
			return fmt.Errorf("download update: %w", err)
		}
		if !verifyFile(newExe, info.SHA256) {
			os.Remove(newExe)
			return fmt.Errorf("update package sha256 mismatch")
		}
	}

	// 由新包自己完成替换：解释器在服务进程里常被终端安全软件拦截，
	// 换成自研二进制做 SCM 停启 + 文件替换，整条链不依赖外部解释器
	if err := spawnApply(newExe, installDir, os.Getpid()); err != nil {
		// 服务进程无可见 stderr，把失败原因落到 update 目录供现场排查
		_ = os.WriteFile(filepath.Join(updateDir, "update.log"),
			[]byte(time.Now().UTC().Format(time.RFC3339)+" spawn failed: "+err.Error()+"\n"), 0o644)
		return fmt.Errorf("spawn apply: %w", err)
	}
	log.Printf("updater spawned, agent exiting for update to v%s", info.Version)
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
