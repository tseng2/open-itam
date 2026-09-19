import os
import glob
import subprocess
import sys
import tempfile

def find_binary():
    user_profile = os.environ.get("USERPROFILE")
    if not user_profile:
        user_profile = os.path.expanduser("~")

    # Standalone install (from installer)
    standalone = os.path.join(user_profile, "AppData", "Local", "Programs", "codebase-memory-mcp", "codebase-memory-mcp.exe")
    if os.path.exists(standalone):
        return standalone

    # Search in AppData Local npm-cache _npx folders
    search_pattern = os.path.join(user_profile, "AppData", "Local", "npm-cache", "_npx", "*", "node_modules", "codebase-memory-mcp", "bin", "codebase-memory-mcp.exe")
    matches = glob.glob(search_pattern)
    if matches:
        return matches[0]
    return "codebase-memory-mcp" # Fallback to system PATH

def main():
    binary = find_binary()

    # We want the repository root directory
    # Since the script is in .zcode/skills/codebase-memory-mcp/scripts/index.py,
    # the repo root is 4 levels up from scripts: scripts -> codebase-memory-mcp -> skills -> .zcode -> repo_root
    script_dir = os.path.dirname(os.path.abspath(__file__))
    repo_root = os.path.abspath(os.path.join(script_dir, "..", "..", "..", ".."))

    # Normalize path to forward slashes to prevent codebase-memory-mcp parsing issues
    repo_path = repo_root.replace("\\", "/")

    if len(sys.argv) > 2:
        # v0.10+: pass tool arguments via --args-file (raw positional JSON is deprecated)
        tool = sys.argv[1]
        args_json = sys.argv[2]
        fd, args_path = tempfile.mkstemp(suffix=".json", prefix="cbm-args-")
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as f:
                f.write(args_json)
            cmd = [binary, "cli", tool, "--args-file", args_path]
            result = subprocess.run(cmd, capture_output=True, text=True)
        finally:
            try:
                os.unlink(args_path)
            except OSError:
                pass
        if result.returncode == 0:
            print(result.stdout)
        else:
            print("Error details:", result.stderr, file=sys.stderr)
            sys.exit(result.returncode)
        return

    cmd = [binary, "cli", "index_repository", "--repo-path", repo_path]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode == 0:
        print(result.stdout)
    else:
        print("Error details:", result.stderr, file=sys.stderr)
        sys.exit(result.returncode)

if __name__ == "__main__":
    main()
