package main

import (
	"flag"
	"fmt"
	"os"
)

// This file defines the CLI structure version is that the binary will report
const version = "3.0.2"

func main() {
	runRoot(os.Args[1:])
}

func runRoot(args []string) {
	f := flag.NewFlagSet("root", flag.ExitOnError)
	f.Usage = usage
	f.Parse(args)

	switch f.Arg(0) {
	case "version":
		runVersion(args[1:])
	case "update":
		runUpdate(args[1:])
	case "snapshot":
		runSnapshot(args[1:])
	default:
		f.Usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `sourcegraph-server-gen assists with the administration of Sourcegraph Data Center

Subcommands:
  version			prints version
  update			updates to a version
  snapshot			creates or restores a snapshot of the database

`)
}

func runVersion(args []string) {
	f := flag.NewFlagSet("sourcegraph-server-gen version", flag.ExitOnError)
	f.Parse(args)
	fmt.Println(version)
}

func runUpdate(args []string) {
	f := flag.NewFlagSet("sourcegraph-server-gen update", flag.ExitOnError)
	f.Usage = func() {
		fmt.Fprintln(os.Stderr, `sourcegraph-server-gen update [version]

If no version is specified, then latest is used.`)
	}
	f.Parse(args)

	if f.NArg() > 1 {
		f.Usage()
		os.Exit(2)
	}

	if err := doUpdate(f.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}
}

func runSnapshot(args []string) {
	f := flag.NewFlagSet("sourcegraph-server-gen snapshot", flag.ExitOnError)
	snapDir := f.String("d", "sourcegraph-snapshot", "snapshot directory")
	redisFlag := f.Bool("redis", true, "include Redis data (default true)")
	postgresFlag := f.Bool("pg", true, "include PostgreSQL data (default true)")
	ignoreSchemaDifference := f.Bool("f", false, "when restoring a snapshot, ignore schema differences and force restore (unsafe)")
	f.Usage = func() {
		fmt.Fprintln(os.Stderr, `sourcegraph-server-gen snapshot [options] {create|restore}`)
		fmt.Fprintln(os.Stderr, "\nOptions:")
		f.PrintDefaults()
	}
	f.Parse(args)

	if f.NArg() != 1 {
		f.Usage()
		os.Exit(2)
	}

	switch f.Arg(0) {
	case "create":
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "createSnapshot failed: %v\n", r)
					os.Exit(2)
				}
			}()
			createSnapshot(*snapDir, *postgresFlag, *redisFlag)
		}()
	case "restore":
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "restoreSnapshot failed: %v\n", r)
					os.Exit(2)
				}
			}()
			restoreSnapshot(*snapDir, *ignoreSchemaDifference, *postgresFlag, *redisFlag)
		}()
	default:
		f.Usage()
		os.Exit(2)
	}
}
