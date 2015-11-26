/*
 * Copyright (C) 2015 Tom Swindell <t.swindell@rubyx.co.uk>
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 2 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License along
 * with this program; if not, write to the Free Software Foundation, Inc.,
 * 51 Franklin Street, Fifth Floor, Boston, MA 02110-1301 USA.
 *
 */
package main

import (
    "flag"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "syscall"

    "github.com/vishvananda/netlink"
)

//   This constant is used as a macro for the argv[0] argument in the new
// spawned process, as a means to detect that it is the forked child process.
const NS_INSTANCE_TAG = "**--ns-instance--**"


// Command line arguments ======================================================
var aNetworkHelper = flag.String("network-helper", "", "Namespace network setup script.")
var aConfigFile = flag.String("config", "", "Namespace configuration file.")
var aNetAddr = flag.String("ipaddr", "", "Namespace IPv4 address.")
var aRootFS = flag.String("rootfs", "", "Namespace rootfs.")
var aId = flag.String("nest-id", "", "Namespace label/id.")
// =============================================================================


// UTILITIES ===================================================================
var empty struct {}

func ShowInfo(m string, args ...interface{}) {
    fmt.Fprintf(os.Stderr, "I:" + m + "\n", args...)
}

func ShowError(m string, e error) {
    fmt.Fprintf(os.Stderr, "E: %s: %v\n", m, e)
}
// =============================================================================


/*   clone() forks a new process calling this executable in a new
 * Linux namespace, this function will not return, until the underlying process
 * has finished.
 */
func clone(path string, args []string) error {
    var config *NamespaceConfig

    // Build configuration, either from specified config file, or defaults.
    if len(*aConfigFile) > 0 {
        f, e := os.Open(*aConfigFile)
        if e != nil { return fmt.Errorf("Failed to load config: %v", e) }
        config, e = GetConfig(f)
        if e != nil { return fmt.Errorf("Failed to load config: %v", e) }
    } else {
        var e error
        config, e = GetConfig(nil)
        if e != nil { return fmt.Errorf("Failed to load config: %v", e) }
    }

    // Override loaded configuration with options supplied via command line.
    if len(*aId) > 0 {
        config.Id = *aId
    }

    if len(*aRootFS) > 0 {
        config.RootFS = *aRootFS
    }

    if len(*aNetAddr) > 0 {
        config.NetworkAddr = *aNetAddr
    }

    nsGuid := config.Id
    //nsFeatures := strings.Join(config.Features, ",")

    nsCloneFlags, e := config.GetCloneFlags()
    if e != nil { return e }

    nsUidMappings, e := config.GetUidMappings()
    if e != nil { return e }

    nsGidMappings, e := config.GetGidMappings()
    if e != nil { return e }

    // Create new process to spawn namespace instance.
    c := &exec.Cmd {
        Path: os.Args[0], // We spawn ourself to do initial setup.
        Args: append([]string {NS_INSTANCE_TAG, path}, args...),

        Env: []string {
            fmt.Sprintf("LXNS_ID=%s", nsGuid),
            //fmt.Sprintf("LXNS_FEATURES=%s", nsFeatures),
        },

        Stdin:  os.Stdin,
        Stdout: os.Stdout,
        Stderr: os.Stderr,

        SysProcAttr: &syscall.SysProcAttr {
             Cloneflags: nsCloneFlags,
            UidMappings: nsUidMappings,
            GidMappings: nsGidMappings,
        },
    }

    // We create new file descriptor (3) in the namespace to inject config data.
    r, w, e := os.Pipe()
    if e != nil {
        return fmt.Errorf("Failed to create control file: %v", e)
    }
    c.ExtraFiles = []*os.File {r}

    // Startup namespace.
    if e := c.Start(); e != nil {
        return fmt.Errorf("Failed to start process: %v", e)
    }

    //   We invoke an external command, which creates the veth iface pair for
    // allowing the namespace access to the bridged network interface. This
    // external command must be setuid and owned by root in order to create
    // these network interfaces.
    networkHelper := *aNetworkHelper

    if len(networkHelper) == 0 {
        if gopath := os.Getenv("GOPATH"); len(gopath) > 0 {
            networkHelper = filepath.Join(gopath, "bin/network-helper")
        }
    }

    if len(networkHelper) > 0 {
        nsnet := exec.Command(networkHelper, strconv.Itoa(c.Process.Pid))
        if o, e := nsnet.CombinedOutput(); e != nil {
            c.Process.Kill()
            return fmt.Errorf("Network helper failed: %v: %s\n", e, o)
        }
    }

    if e := config.Encode(w); e != nil {
        return fmt.Errorf("Failed to send config data: %v", e)
    }

    return c.Wait()
}

/*   setup() is tasked with running the setup code from within the namespace,
 * before the target executable is invoked. It is designed to only execute the
 * correct hooks for the features the namespace was created with.
 */
func setup() error {
    // Read in container environment variables.
    nsId,_ := syscall.Getenv("LXNS_ID")

    // Read in container configuration.
    config, e := GetConfig(os.NewFile(3, "-"))
    if e != nil {
        return fmt.Errorf("Failed to import configuration: %v", e)
    }

    // Create a key set to check flags against.
    nsCloneFlags := make(map[string]struct{})
    for _, v := range config.Features { nsCloneFlags[v] = empty }

    // Do we have networking namespace?
    if _, ok := nsCloneFlags["net"]; ok {
        if e := SetupNetwork(config.NetworkAddr); e != nil { return e }
    }

    // Do we have mounting namespace?
    if _, ok := nsCloneFlags["ns"]; ok {
        // If we have PID namespace, then mount /proc
        if _, ok := nsCloneFlags["pid"]; ok {
            if e := syscall.Mount("proc", "/proc", "proc",
                                  syscall.MS_NOEXEC | syscall.MS_NOSUID | syscall.MS_NODEV,
                                  ""); e != nil {
                return fmt.Errorf("Failed to mount proc: %v", e)
            }
        }

        // Change our root filesystem using pivot_root syscall
        if len(config.RootFS) > 0 {
            PivotRoot(config.RootFS)
        }
    }

    // Do we have hostname namespace?
    if _, ok := nsCloneFlags["uts"]; ok {
        hostname := fmt.Sprintf("lxns-%s", nsId)
        if e := syscall.Sethostname([]byte(hostname)); e != nil {
            return e
        }
    }
    return nil
}

/*   BindMount()
 */
func BindMount(src, dst string) error {
    if e := syscall.Mount(src, dst, "bind",
                          syscall.MS_BIND | syscall.MS_REC,
                          ""); e != nil {
        return fmt.Errorf("Failed to bind mount dev: %v", e)
    }

    return nil
}

/*   PivotRoot()
 */
func PivotRoot(rootfs string) error {
    pivotRoot := filepath.Join(rootfs, ".pivot_root")

    if e := BindMount("/dev", filepath.Join(rootfs, "dev"));
       e != nil {
        return fmt.Errorf("Failed to bind mount dev: %v", e)
    }

    if e := BindMount("/proc", filepath.Join(rootfs, "proc"));
       e != nil {
        return fmt.Errorf("Failed to bind mount dev: %v", e)
    }

    if e := BindMount("/sys", filepath.Join(rootfs, "sys"));
       e != nil {
        return fmt.Errorf("Failed to mount sysfs: %v", e)
    }

    if e := BindMount(rootfs, rootfs);
       e != nil {
        return fmt.Errorf("Failed to bind mount rootfs: %v", e)
    }

    if _, e := os.Stat(pivotRoot); os.IsNotExist(e) {
        if e := os.Mkdir(pivotRoot, 755); e != nil {
            return fmt.Errorf("Failed to make .pivot_root dir: %v", e)
        }
    }

    if e := syscall.PivotRoot(rootfs, pivotRoot); e != nil {
        return fmt.Errorf("Failed to pivot_root: %v", e)
    }

    if e := syscall.Chdir("/"); e != nil {
        return fmt.Errorf("Failed to cd to /: %v", e)
    }

    if e := syscall.Unmount("/.pivot_root", syscall.MNT_DETACH); e != nil {
        return fmt.Errorf("Failed to unmount pivot_root dir: %v", e)
    }

    if e := os.Remove("/.pivot_root"); e != nil {
        return fmt.Errorf("Failed to rmdir /.pivot_root: %v", e)
    }

    return nil
}

/*   SetupNetwork()
 */
func SetupNetwork(addr string) error {
    // Bring up loop back interface.
    lo, e := netlink.LinkByName("lo")
    if e != nil {
        return fmt.Errorf("Failed to find loopback interface: %v", e)
    }

    if e := netlink.LinkSetUp(lo); e != nil {
        return fmt.Errorf("Failed to setup loopback interface: %v", e)
    }

    if len(addr) > 0 {
        veth, e := netlink.LinkByName("veth0")
        if e != nil {
            return fmt.Errorf("Failed to find veth interface: %v", e)
        }

        addr, e := netlink.ParseAddr(addr)
        if e != nil {
            return fmt.Errorf("Failed to parse NetworkAddr: %v", e)
        }

        netlink.AddrAdd(veth, addr)
        if e := netlink.LinkSetUp(veth); e != nil {
            return fmt.Errorf("Network link failed to come up: %v", e)
        }
    }

    return nil
}

/*
 * main() - !!! GO GO GO !!!
 */
func main() {
    if os.Args[0] == NS_INSTANCE_TAG {
        // Configure/Setup namespace parts from within container.
        if e := setup(); e != nil {
            ShowError("Unable to configure namespace", e)
            os.Exit(1)
        }

        if _, e := exec.LookPath(os.Args[1]); e != nil {
            ShowError("Executable not found", e)
            os.Exit(1)
        }

        // Check to see that the target executable exists.
        if _, e := exec.LookPath(os.Args[1]); e != nil {
            ShowError("Could not find target executable", e)
            os.Exit(1)
        }

        // Execute target process.
        if e := syscall.Exec(os.Args[1], os.Args[1:], os.Environ()); e != nil {
            ShowError("Failed to exec child process", e)
            os.Exit(1)
        }
        os.Exit(0)
    }

    // Parse command-line arguments
    flag.Parse()

    if len(os.Args) <= 1 {
        fmt.Fprintf(os.Stderr, "Executable path not specified.\n")
        os.Exit(1)
    }

    if e := clone(flag.Arg(0), flag.Args()[1:]); e != nil {
        ShowError("Error creating namespace", e)
        os.Exit(1)
    }
}

