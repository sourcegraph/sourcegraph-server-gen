package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	schemaVersionFile = "schema.txt"
	sqlFile           = "snapshot.sql"
)

type schemaMigrations struct {
	Version int64 `json:"version"`
	Dirty   bool  `json:"dirty"`
}

func getSchemaVersion(pgsqlPod string) schemaMigrations {
	var m []schemaMigrations
	mBytes := execfBytes(`kubectl exec %s -- psql -U sg -t -c 'select array_to_json(array_agg(schema_migrations)) from schema_migrations'`, pgsqlPod)
	if err := json.Unmarshal(mBytes, &m); err != nil {
		panic(err)
	}
	if len(m) != 1 {
		panic(fmt.Sprintf("expected exactly 1 row in schema_migrations, found %d", len(m)))
	}
	if m[0].Dirty {
		panic("schema_migrations table is dirty")
	}
	return m[0]
}

func createSnapshot(outDir string, includePostgres, includeRedis bool) {
	verify()
	if err := os.MkdirAll(outDir, 0777); err != nil {
		panic(err)
	}
	fmt.Printf("Snapshotting cluster (pg=%v, redis=%v) %s to %s\n", includePostgres, includeRedis, strings.TrimSpace(execStr(`kubectl config current-context`)), outDir)
	if includePostgres {
		createPGSnapshot(outDir)
	}
	if includeRedis {
		createRedisSnapshot(outDir)
	}
}

func createPGSnapshot(outDir string) {
	fmt.Fprintln(os.Stderr, "Creating PostgreSQL snapshot...")
	pgsqlPod := execStr(`kubectl get pods -l app=pgsql -o jsonpath={.items[0].metadata.name}`)

	schema := getSchemaVersion(pgsqlPod)
	err := ioutil.WriteFile(
		filepath.Join(outDir, schemaVersionFile),
		[]byte(fmt.Sprintf("%d", schema.Version)),
		0666,
	)
	if err != nil {
		panic(err)
	}

	sqlOut, err := os.Create(filepath.Join(outDir, sqlFile))
	if err != nil {
		panic(err)
	}
	defer sqlOut.Close()

	execf(sqlOut, `kubectl exec %s -- pg_dump -a -U sg `+pgdumpTablesExpr(getAllTables(pgsqlPod)), pgsqlPod)
	fmt.Fprintln(os.Stderr, "Created PostgreSQL snapshot.")
}

func getAllTables(pgsqlPod string) []string {
	b := execfBytes(`kubectl exec %s -- psql -t -U sg -c "%s"`, pgsqlPod, `SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE' AND table_name != 'schema_migrations'`)
	lines := strings.Split(string(b), "\n")
	var tables []string
	for _, line := range lines {
		if len(line) > 0 {
			tables = append(tables, strings.TrimSpace(line))
		}
	}
	return tables
}

func pgdumpTablesExpr(tbls []string) string {
	exprs := make([]string, len(tbls))
	for i, tbl := range tbls {
		exprs[len(tbls)-i-1] = fmt.Sprintf("-t %s", tbl)
	}
	return strings.Join(exprs, " ")
}

func truncateSQL(tables []string) string {
	return "TRUNCATE " + strings.Join(tables, ", ") + " RESTART IDENTITY"
}

func restoreSnapshot(snapDir string, ignoreSchemaDifference bool, includePostgres, includeRedis bool) {
	verify()
	fmt.Printf("\n\t!!! IMPORTANT: Make sure you've set sourcegraph-frontend replica count to 0 before running this operation.\n")
	fmt.Printf("About to restore snapshot to cluster (pg=%v, redis=%v) %s from %s\n", includePostgres, includeRedis, strings.TrimSpace(execStr(`kubectl config current-context`)), snapDir)
	fmt.Print("Clear existing data and restore from snapshot? (This operation cannot be undone.) [y/N] ")
	in := bufio.NewReader(os.Stdin)
	line, _, err := in.ReadLine()
	if err != nil {
		panic(err)
	}
	if userIn := strings.ToLower(strings.TrimSpace(string(line))); userIn != "y" {
		fmt.Fprintln(os.Stderr, "Aborting")
		os.Exit(1)
	}
	if includePostgres {
		restorePGSnapshot(snapDir, ignoreSchemaDifference)
	}
	if includeRedis {
		restoreRedisSnapshot(snapDir)
	}
}

func restorePGSnapshot(snapDir string, ignoreSchemaDifference bool) {
	fmt.Printf("Restoring Postgres\n")
	snapFile := filepath.Join(snapDir, sqlFile)
	versionFile := filepath.Join(snapDir, schemaVersionFile)
	for _, reqdFile := range []string{snapFile, versionFile} {
		if finfo, err := os.Stat(reqdFile); err != nil || finfo.IsDir() {
			panic(fmt.Sprintf("%s is not a regular file", reqdFile))
		}
	}

	pgsqlPod := execStr(`kubectl get pods -l app=pgsql -o jsonpath={.items[0].metadata.name}`)

	schema := getSchemaVersion(pgsqlPod)
	versionBytes, err := ioutil.ReadFile(versionFile)
	if err != nil {
		panic(err)
	}
	snapVersion, err := strconv.ParseInt(string(versionBytes), 10, 0)
	if err != nil {
		panic(err)
	}
	if schema.Version != snapVersion && !ignoreSchemaDifference {
		panic(fmt.Sprintf("snapshot schema version (%d) differs from current schema version (%d)", snapVersion, schema.Version))
	}

	fmt.Println("Clearing current data...")
	execfBytes(`kubectl exec %s -- psql -U sg -c '%s'`, pgsqlPod, truncateSQL(getAllTables(pgsqlPod)))
	fmt.Printf("Restoring from %s...\n", snapDir)
	execfBytes(`cat %s | kubectl exec -i %s -- psql -U sg`, snapFile, pgsqlPod)
}

var snapshotRequiredCommands = []string{"bash", "kubectl", "cat"}

func verify() {
	for _, reqdCmd := range snapshotRequiredCommands {
		if _, err := exec.LookPath(reqdCmd); err != nil {
			panic(fmt.Sprintf("required command %q is missing", reqdCmd))
		}
	}
}

func execStr(command string) string {
	var out bytes.Buffer
	execCmd(&out, command)
	return out.String()
}

func execBytes(command string) []byte {
	var out bytes.Buffer
	execCmd(&out, command)
	return out.Bytes()
}

func execfBytes(commandFormat string, args ...interface{}) []byte {
	return execBytes(fmt.Sprintf(commandFormat, args...))
}

func execf(out io.Writer, commandFormat string, args ...interface{}) {
	execCmd(out, fmt.Sprintf(commandFormat, args...))
}

func execCmd(out io.Writer, command string) {
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = out
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Command `%s` failed, error: %s, stderr:\n%s===============\n", command, err, errBuf.String())
		panic(err)
	}
}
