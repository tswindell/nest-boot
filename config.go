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
    "fmt"
    "encoding/json"
    "errors"
    "io"
    "os"
    "syscall"
)

type Exec struct {
    Path string
    Args []string
}

type IdMapping struct {
    ContainerId int
    HostId int
    Size int
}

type Mount struct {
    Device  string
    Target  string
    Type    string
    Flags   []string
    Options string
}

type NamespaceConfig struct {
    Id string
    Features []string

    UidMappings []IdMapping
    GidMappings []IdMapping

    RootFS string

    Mounts []Mount

    NetworkAddr string

    Exec Exec
}

//TODO: Implement reading from STDIN, without losing it on EOF for child process.
func GetConfig(f *os.File) (*NamespaceConfig, error) {
    // Create a new config object with defaults.
    c := &NamespaceConfig {
        Id: "0123456789", // TODO: Auto-Generate ...
        Features: []string {"ipc", "net", "ns", "pid", "user", "uts"},

        //FIXME:
        //  Having a hard time getting uid/gid mappings working with subuids & subgids.
        //  This could be an issue with some security policy on my machine maybe.
        UidMappings: []IdMapping {
            IdMapping {ContainerId: 0, HostId: os.Getuid(), Size: 1},
        },
        GidMappings: []IdMapping {
            IdMapping {ContainerId: 0, HostId: os.Getgid(), Size: 1},
        },
    }

    if f != nil {
        d := json.NewDecoder(f)
        if e := d.Decode(c); e != nil {
            return nil, fmt.Errorf("Failed to load configuration: %v", e)
        }
    }

    return c, nil
}

func (c *NamespaceConfig) Dump() {
    fmt.Fprintf(os.Stderr, "%v\n", c)
}

func (c *NamespaceConfig) Encode(w io.Writer) error {
    enc := json.NewEncoder(w)
    if e := enc.Encode(c); e != nil {
        return e
    }
    return nil
}

func (c *NamespaceConfig) GetCloneFlags() (uintptr, error) {
    // Attribute keys to syscall interface flag map.
    attrMap := map[string]int {
         "ipc": syscall.CLONE_NEWIPC,
         "net": syscall.CLONE_NEWNET,
          "ns": syscall.CLONE_NEWNS,
         "pid": syscall.CLONE_NEWPID,
        "user": syscall.CLONE_NEWUSER,
         "uts": syscall.CLONE_NEWUTS,
    }

    var result uintptr

    for _, v := range c.Features {
        j, c := attrMap[v]
        if !c {
            return 0, errors.New(fmt.Sprintf("Unrecognised clone flag: %s", v))
        }
        result |= uintptr(j)
    }

    return result, nil
}

func (c *NamespaceConfig) GetUidMappings() ([]syscall.SysProcIDMap, error) {
    result := []syscall.SysProcIDMap {}

    for _, v := range c.UidMappings {
        result = append(result, syscall.SysProcIDMap {
            ContainerID: v.ContainerId,
            HostID: v.HostId,
            Size: v.Size,
        })
    }

    return result, nil
}

func (c *NamespaceConfig) GetGidMappings() ([]syscall.SysProcIDMap, error) {
    result := []syscall.SysProcIDMap {}

    for _, v := range c.GidMappings {
        result = append(result, syscall.SysProcIDMap {
            ContainerID: v.ContainerId,
            HostID: v.HostId,
            Size: v.Size,
        })
    }

    return result, nil
}

