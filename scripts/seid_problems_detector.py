#!/usr/bin/env python3
import os
import platform
import re
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Optional, Tuple

def style_ok(msg):   return f"OK: {msg}"
def style_warn(msg): return f"WARN: {msg}"
def style_fail(msg): return f"FAIL: {msg}"
def head(msg):       return f"\n{msg}"

def have(cmd: str) -> bool:
    return shutil.which(cmd) is not None

def run(cmd: list[str]) -> Tuple[int, str, str]:
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, check=False)
        return p.returncode, p.stdout.strip(), p.stderr.strip()
    except Exception as e:
        return 1, "", str(e)

def bytes_to_gb_floor(b: int) -> int:
    return int(b // (1024**3))

def read_os_release() -> dict:
    d = {}
    try:
        for line in Path("/etc/os-release").read_text().splitlines():
            if "=" in line:
                k, v = line.split("=", 1)
                d[k] = v.strip().strip('"').strip("'")
    except Exception:
        pass
    return d

def mem_total_gb() -> int:
    try:
        import psutil  # type: ignore
        return bytes_to_gb_floor(psutil.virtual_memory().total)
    except Exception:
        pass
    try:
        with open("/proc/meminfo", "r") as fh:
            for line in fh:
                if line.startswith("MemTotal:"):
                    kb = int(re.findall(r"\d+", line)[0])
                    return bytes_to_gb_floor(kb * 1024)
    except Exception:
        pass
    try:
        pages = os.sysconf("SC_PHYS_PAGES")
        page_size = os.sysconf("SC_PAGE_SIZE")
        return bytes_to_gb_floor(pages * page_size)
    except Exception:
        pass
    return 0

def cpu_cores() -> int:
    return os.cpu_count() or 1

def find_default_sei_home() -> Optional[Path]:
    for c in [Path.home()/".sei", Path("/root/.sei")]:
        if c.is_dir():
            return c
    return None

def findmnt_source_and_fstype(target: Path) -> Tuple[Optional[str], Optional[str]]:
    probe = target if target.exists() else target.parent
    if not have("findmnt"):
        return None, None
    rc, src, _ = run(["findmnt", "-n", "-o", "SOURCE", "--target", str(probe)])
    rc2, fstype, _ = run(["findmnt", "-n", "-o", "FSTYPE", "--target", str(probe)])
    return (src if rc == 0 and src else None, fstype if rc2 == 0 and fstype else None)

def root_block_for(mount_src: str) -> Optional[str]:
    if not mount_src.startswith("/dev/"):
        return None
    dev = Path(mount_src).name
    if dev.startswith("mapper/") or mount_src.startswith("/dev/mapper/"):
        if have("lsblk"):
            rc, out, _ = run(["lsblk", "-no", "PKNAME", mount_src])
            if rc == 0 and out:
                return out.splitlines()[0].strip()
    return re.sub(r"p?\d+$", "", dev)

def assess_storage(block: Optional[str]) -> str:
    if not block:
        return "class=unknown;reason=no_block"
    if block.startswith("nvme"):
        return "class=nvme;reason=block device name starts with nvme"
    return "class=not-nvme;reason=block device name does not start with nvme"

def looks_like_kubernetes_or_container() -> bool:
    try:
        if Path("/var/run/secrets/kubernetes.io/serviceaccount/token").exists(): return True
        if Path("/proc/self/cgroup").exists():
            text = Path("/proc/self/cgroup").read_text(errors="ignore")
            if re.search(r"kubepods|containerd|cri-containerd|crio|podman|docker|lxc", text):
                return True
        if Path("/.dockerenv").exists() or Path("/.containerenv").exists(): return True
        if os.environ.get("KUBERNETES_SERVICE_HOST"): return True
    except Exception:
        pass
    return False

def ensure_ubuntu() -> Tuple[str, str, str]:
    osinfo = read_os_release()
    os_id = osinfo.get("ID", "unknown")
    name = osinfo.get("NAME", platform.platform())
    if os_id == "ubuntu":
        return "pass", f"Detected: {name} (ID={os_id})", ""
    else:
        return "fail", f"Detected: {name} (ID={os_id})", f"Not Ubuntu (ID={os_id}). Recommended: Ubuntu."

def parse_go_duration_to_seconds(s: str) -> Optional[float]:
    if not isinstance(s, str) or not s.strip():
        return None
    pattern = re.compile(r"(?P<val>[-+]?(?:\d+(?:\.\d*)?|\.\d+))(?P<unit>ns|us|µs|ms|s|m|h)")
    total_seconds = 0.0
    for m in pattern.finditer(s.strip().lower()):
        val = float(m.group("val"))
        unit = m.group("unit")
        total_seconds += val / 1_000_000_000 if unit == "ns" else \
                         val / 1_000_000 if unit in ("us", "µs") else \
                         val / 1000 if unit == "ms" else \
                         val if unit == "s" else \
                         val * 60 if unit == "m" else \
                         val * 3600
    return total_seconds

def load_mempool_config_from_toml(path: Path) -> Tuple[Optional[str], Optional[int]]:
    try:
        import tomllib  # type: ignore
        data = tomllib.loads(path.read_text())
    except Exception:
        try:
            import tomli  # type: ignore
            data = tomli.loads(path.read_text())
        except Exception:
            text = path.read_text()
            sec = re.search(r"^\s*\[mempool\]\s*$([\s\S]*?)(?=^\s*\[[^\]]+\]\s*$|\Z)", text, re.MULTILINE)
            if not sec: return None, None
            body = sec.group(1)
            m1 = re.search(r"^\s*ttl[-_]duration\s*=\s*([^\n#]+)", body, re.MULTILINE)
            td = m1.group(1).strip().strip('"').strip("'") if m1 else None
            m2 = re.search(r"^\s*ttl[-_]num[-_]blocks\s*=\s*([^\n#]+)", body, re.MULTILINE)
            tnb = int(m2.group(1).strip()) if m2 else None
            return td, tnb
    mem = data.get("mempool", {}) if isinstance(data, dict) else {}
    td = mem.get("ttl-duration") or mem.get("ttl_duration")
    tnb = mem.get("ttl-num-blocks") or mem.get("ttl_num_blocks")
    return str(td) if td is not None else None, int(tnb) if tnb is not None else None

def main():
    failures = 0
    print(head("Seid Problem Detector"))
    status, msg, extra = ensure_ubuntu()
    if status == "fail":
        print(style_fail(f"{extra} {msg}")); failures += 1
    else:
        print(style_ok("Running on Ubuntu."))
    cores = cpu_cores()
    if cores < 16:
        print(style_fail(f"CPU cores ({cores}) below required minimum (16).")); failures += 1
    else:
        print(style_ok(f"CPU cores OK: {cores}"))
    ram = mem_total_gb()
    if ram < 256:
        print(style_fail(f"RAM ({ram} GB) below required minimum (256 GB).")); failures += 1
    else:
        print(style_ok(f"RAM OK: {ram} GB"))
    default_home = find_default_sei_home()
    if not default_home:
        print(style_fail("Could not find '.sei' directory in current user's home or in /root.")); failures += 1
    else:
        print(style_ok(f"Found '.sei' directory at: {default_home}"))
        mount_src, _ = findmnt_source_and_fstype(default_home)
        if not mount_src or not mount_src.startswith("/dev/"):
            print(style_fail(".sei directory is not on a block device.")); failures += 1
        else:
            block = root_block_for(mount_src)
            info = assess_storage(block)
            if not info.startswith("class=nvme"):
                print(style_fail(f".sei directory must be on local NVMe storage. {info}")); failures += 1
            else:
                print(style_ok(f".sei directory is on local NVMe storage"))
        cfg = default_home / "config" / "config.toml"
        if not cfg.is_file():
            print(style_fail(f"Missing config file: {cfg}")); failures += 1
        else:
            print(style_ok(f"Found config file: {cfg}"))
            td_str, tnb_int = load_mempool_config_from_toml(cfg)
            secs = parse_go_duration_to_seconds(td_str) if td_str else None
            if td_str is None or secs is None or secs <= 2:
                print(style_fail(f"mempool.ttl-duration must be > 2s (got {td_str}).")); failures += 1
            else:
                print(style_ok(f"mempool.ttl-duration OK (>2s): {td_str}"))
            if tnb_int is None or tnb_int <= 5:
                print(style_fail(f"mempool.ttl-num-blocks must be > 5 (got {tnb_int}).")); failures += 1
            else:
                print(style_ok(f"mempool.ttl-num-blocks OK (>5): {tnb_int}"))
    if looks_like_kubernetes_or_container():
        print(style_fail("Environment appears to be Kubernetes/containerized.")); failures += 1
    else:
        print(style_ok("Not running in Kubernetes/containerized environment."))
    print("\nDone. Review messages above.")
    sys.exit(1 if failures > 0 else 0)

if __name__ == "__main__":
    main()
