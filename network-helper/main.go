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
    "os"
    "strconv"
    "github.com/vishvananda/netlink"
)

/*   setup()
 */
func setup(pid int) error {
    br, e := netlink.LinkByName("vbr0")
    if e != nil {
        return fmt.Errorf("Host bridge not found: %v", e)
    }

    // Network interface names.
    hsIfName := fmt.Sprintf("veth%d", pid) // Host side interface
    nsIfName := "veth0"

    // Assign host side interface to bridge.
    linkAttrs := netlink.NewLinkAttrs()
    linkAttrs.Name = hsIfName
    linkAttrs.MasterIndex = br.Attrs().Index

    // Create interface pair.
    hsIf := &netlink.Veth {LinkAttrs: linkAttrs, PeerName: nsIfName}
    if e := netlink.LinkAdd(hsIf); e != nil {
        return fmt.Errorf("Failed to create veth pair: %v", e)
    }

    // Get namespace side interface handle.
    nsIf, e := netlink.LinkByName(nsIfName)
    if e != nil {
        netlink.LinkDel(hsIf)
        return fmt.Errorf("Failed to get namespace iface: %v", e)
    }

    // Attach network interface to namespace.
    if e := netlink.LinkSetNsPid(nsIf, pid); e != nil {
        netlink.LinkDel(hsIf)
        return fmt.Errorf("Failed to attach namespace iface: %v", e)
    }

    // Bring up host side interface.
    if e := netlink.LinkSetUp(hsIf); e != nil {
        netlink.LinkDel(hsIf)
        return fmt.Errorf("Failed to bring up host iface: %v", e)
    }

    return nil
}

/*   main()
 */
func main() {
    if len(os.Args) != 2 {
        os.Exit(1)
    }

    pid, e := strconv.Atoi(os.Args[1])
    if e != nil {
        fmt.Fprintf(os.Stderr, "Failed to parse PID from args.")
        os.Exit(1)
    }

    fmt.Fprintf(os.Stderr, "PID: %d\n", pid)
    if e := setup(pid); e != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", e)
        os.Exit(1)
    }

    os.Exit(0)
}

