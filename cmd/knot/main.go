package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/knot-infra/knot/pkg/client"
	"golang.org/x/term"
)

type config struct {
	APIURL       string `json:"api_url"`
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Email        string `json:"email,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "login":
		cmdLogin()
	case "status":
		cmdStatus()
	case "devices":
		cmdDevices()
	case "device":
		if len(os.Args) < 4 || os.Args[2] != "show" {
			fmt.Fprintln(os.Stderr, "usage: knot device show <id>")
			os.Exit(1)
		}
		cmdDeviceShow(os.Args[3])
	case "reg-token", "registration-token":
		cmdRegToken()
	case "transfer":
		cmdTransfer(os.Args[2:])
	case "storage":
		cmdStorage(os.Args[2:])
	case "sync":
		cmdSync(os.Args[2:])
	case "files":
		cmdFiles(os.Args[2:])
	case "service", "services":
		cmdServices(os.Args[2:])
	case "route", "routes":
		cmdRoutes(os.Args[2:])
	case "deploy", "deployment", "deployments":
		cmdDeploy(os.Args[2:])
	case "backup":
		cmdBackup()
	case "compute":
		cmdCompute(os.Args[2:])
	case "job", "jobs":
		cmdJobs(os.Args[2:])
	case "env", "environment", "environments":
		cmdEnv(os.Args[2:])
	case "secret", "secrets":
		cmdSecret(os.Args[2:])
	case "source", "sources":
		cmdSource(os.Args[2:])
	case "build", "builds":
		cmdBuild(os.Args[2:])
	case "release", "releases":
		cmdRelease(os.Args[2:])
	case "traffic":
		cmdTraffic(os.Args[2:])
	case "logs":
		cmdLogs(os.Args[2:])
	case "ops":
		cmdOps(os.Args[2:])
	case "workflow", "workflows":
		cmdWorkflow(os.Args[2:])
	case "ai":
		cmdAI(os.Args[2:])
	case "plan", "plans":
		cmdPlan(os.Args[2:])
	case "audit":
		cmdAudit(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`knot — Node CLI (codename knot)

Usage:
  knot login
  knot status
  knot devices
  knot device show <id>
  knot reg-token [--name NAME]
  knot transfer send --from <id> --to <id> --file <name> --sha256 <hex> --size <n> [--path <rel>]
  knot transfer status <id>
  knot transfer list
  knot storage ls --device <id> [--path <rel>]
  knot storage stat --device <id> --path <rel>
  knot storage mkdir --device <id> --path <rel>
  knot storage rm --device <id> --path <rel>
  knot storage mv --device <id> --from <rel> --to <rel>
  knot storage copy --device <id> --from <rel> --to <rel>
  knot storage upload --device <id> --path <rel> --from <id> --file <outbox-rel> --sha256 <hex> --size <n> [--resume]
  knot storage download --device <id> --path <rel> --to <id>
  knot sync create --from <id> --from-path <rel> --to <id> --to-path <rel> [--mode one_way|two_way] [--name NAME]
  knot sync list
  knot sync show <id>
  knot sync run <id>
  knot sync cancel <id>
  knot sync rm <id>
  knot sync wait <id>
  knot sync conflicts <job-id>
  knot sync resolve <conflict-id> --resolution keep_a|keep_b|keep_both
  knot sync flush <device-id>
  knot files search [--q TEXT] [--device ID] [--type image|video|pdf|text] [--folder REL] [--min-size N] [--max-size N]
  knot files reindex [--device ID]
  knot services ls [--device ID]
  knot services tree
  knot services add --device ID --name NAME --port N [--kind web|api|database|worker|other] [--protocol http|https|tcp|udp] [--bind 127.0.0.1]
  knot services show <id>
  knot services health <id>
  knot services rm <id>
  knot routes ls
  knot routes add --host HOST --service ID [--edge DEVICE] [--tls-mode edge_terminate|origin_tls]
  knot routes rm <id>
  knot deploy ls [--device ID] [--name NAME]
  knot deploy create --device ID --name|--service NAME --image IMAGE --port N [--environment NAME] [--health /health] [--hostname HOST] [--edge DEVICE]
  knot deploy show <id>
  knot deploy stop <id>
  knot deploy restart <id>
  knot deploy rollback <id>
  knot deploy logs <id>
  knot env ls [--project NAME]
  knot env create NAME [--project NAME] [--var K=V] [--secret KEY=SECRET]
  knot env show <id>
  knot secret ls
  knot secret create NAME [--value VALUE]
  knot secret rotate <id-or-name> [--value VALUE]
  knot source add --url URL [--branch main] [--tag TAG] [--name NAME] [--secret ID]
  knot source ls
  knot source show <id>
  knot build run <source-id> --device ID --tag IMAGE [--dockerfile Dockerfile] [--context .] [--wait]
  knot build ls [--source ID] [--device ID]
  knot build show <id>
  knot build logs <id>
  knot release ls [--service NAME]
  knot release create --service NAME --image IMAGE [--environment NAME] [--device ID] [--port N] [--build ID]
  knot release deploy <id> [--device ID] [--port N]
  knot release show <id>
  knot release logs <id>
  knot release rollback <id>
  knot traffic show <hostname|id>
  knot traffic switch --route HOST|--id ID --release ID [--weight 100]
  knot traffic rollback <hostname|id>
  knot logs list [--service NAME] [--release ID] [--source SRC] [--trace ID]
  knot logs service <name>
  knot logs follow [--service NAME] [--release ID] [--source SRC]
  knot logs release <id>
  knot ops context <service>
  knot workflow ls
  knot workflow run diagnose-service --service NAME
  knot workflow run deploy-release --service NAME --image IMAGE --device ID --port N
  knot workflow run restore-backup [--query backup] [--device ID] [--image IMAGE]
  knot workflow show <id>
  knot ai session create --scope logs.read --scope release.read --ttl 30m
  knot ai session ls
  knot ai session revoke <id>
  knot plan list
  knot plan show <id>
  knot plan create --name update-production --service NAME --image IMAGE [--hostname HOST]
  knot plan approve <id>
  knot plan execute <id>
  knot plan cancel <id>
  knot audit search [--action traffic.switch] [--actor-type ai_session] [--session ID] [--trace ID]
  knot audit ai [--session ID]
  knot audit trace <trace_id>
  knot backup
  knot compute ls
  knot compute show <device-id>
  knot compute labels <device-id> --set k=v[,k=v]
  knot jobs ls [--device ID]
  knot jobs run [--device ID] --image IMAGE [--cpu N] [--memory-mb N] [--gpu N|required] [--disk-mb N] [--pids N] [--timeout N] [--command JSON] [--input PATH] [--require k=v] [--prefer k=v] [--wait]
  knot jobs show <id>
  knot jobs artifacts <id>
  knot jobs logs <id>
  knot jobs cancel <id>
  knot jobs wait <id>

Config: ~/.knot/config.json`)
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".knot", "config.json")
}

func loadConfig() (*config, error) {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return nil, err
	}
	var c config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveConfig(c *config) error {
	dir := filepath.Dir(configPath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(configPath(), b, 0o600)
}

func cmdLogin() {
	reader := bufio.NewReader(os.Stdin)
	apiURL := envOr("KNOT_API_URL", "http://127.0.0.1:8787")
	fmt.Printf("API URL [%s]: ", apiURL)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line != "" {
		apiURL = line
	}
	fmt.Print("Email: ")
	email, _ := reader.ReadString('\n')
	email = strings.TrimSpace(email)
	fmt.Print("Password: ")
	pwBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		fatal(err)
	}
	cl := client.New(apiURL, "")
	res, err := cl.Login(context.Background(), email, string(pwBytes))
	if err != nil {
		fatal(err)
	}
	cfg := &config{
		APIURL:       apiURL,
		Token:        res.AccessToken,
		RefreshToken: res.RefreshToken,
		Email:        res.User.Email,
	}
	if err := saveConfig(cfg); err != nil {
		fatal(err)
	}
	fmt.Printf("Logged in as %s\n", res.User.Email)
}

func apiClient() *client.Client {
	cfg := mustConfig()
	return client.New(cfg.APIURL, cfg.Token)
}

func cmdStatus() {
	cl := apiClient()
	ctx := context.Background()
	if err := cl.Readyz(ctx); err != nil {
		fmt.Printf("Control Plane: not ready (%v)\n", err)
		os.Exit(1)
	}
	me, err := cl.Me(ctx)
	if err != nil {
		fmt.Printf("Control Plane: unreachable or unauthorized (%v)\n", err)
		os.Exit(1)
	}
	cfg := mustConfig()
	fmt.Printf("Control Plane: %s OK\n", cfg.APIURL)
	fmt.Printf("User: %s\n", me.Email)
	fmt.Printf("Actor: %s\n", me.Actor)
}

func cmdBackup() {
	cl := apiClient()
	path, err := cl.Backup(context.Background())
	if err != nil {
		fatal(err)
	}
	fmt.Printf("backup %s\n", path)
}

func cmdCompute(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot compute ls|show|labels <device-id>"))
	}
	cl := apiClient()
	ctx := context.Background()
	switch args[0] {
	case "ls", "list":
		list, err := cl.ListComputeDevices(ctx)
		if err != nil {
			fatal(err)
		}
		if len(list) == 0 {
			fmt.Println("No devices.")
			return
		}
		for _, d := range list {
			fmt.Printf("%s  %s  %s  cpu=%s  ram=%s  gpu=%s  %s\n",
				d.Status, d.Name, d.OS, cpuShort(d.CPU), memShort(d.Memory), gpuShort(d.GPU), d.DeviceID)
		}
	case "show":
		id := ""
		if len(args) >= 2 {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot compute show <device-id>"))
		}
		d, err := cl.GetComputeDevice(ctx, id)
		if err != nil {
			fatal(err)
		}
		printCompute(d)
	case "labels":
		id := ""
		if len(args) >= 2 {
			id = args[1]
		}
		flags := map[string]string{}
		parseFlags(args[1:], flags)
		if flags["device"] != "" {
			id = flags["device"]
		}
		if id == "" || strings.HasPrefix(id, "--") {
			fatal(fmt.Errorf("usage: knot compute labels <device-id> --set k=v[,k=v]"))
		}
		labels := parseLabelList(flags["set"])
		d, err := cl.SetComputeLabels(ctx, id, labels)
		if err != nil {
			fatal(err)
		}
		printCompute(d)
	default:
		fatal(fmt.Errorf("unknown compute subcommand: %s", args[0]))
	}
}

func printCompute(d *client.ComputeDevice) {
	fmt.Printf("%s\n", d.Name)
	fmt.Printf("  status     %s\n", d.Status)
	fmt.Printf("  os         %s/%s\n", d.OS, d.Arch)
	fmt.Printf("  agent      %s\n", emptyDash(d.AgentVersion))
	fmt.Printf("  telemetry  %s\n", emptyDash(ptrStr(d.LastTelemetryAt)))
	if d.CPU != nil {
		load := "—"
		if d.CPU.UsagePercent != nil {
			load = fmt.Sprintf("%.0f%%", *d.CPU.UsagePercent)
		}
		fmt.Printf("  cpu        %d cores  %s  load %s\n", d.CPU.Cores, d.CPU.Architecture, load)
	} else {
		fmt.Printf("  cpu        —\n")
	}
	if d.Memory != nil && d.Memory.TotalBytes > 0 {
		fmt.Printf("  ram        %s total  %s available\n", humanBytes(d.Memory.TotalBytes), humanBytes(d.Memory.AvailableBytes))
	} else {
		fmt.Printf("  ram        —\n")
	}
	fmt.Printf("  gpu        %s\n", gpuShort(d.GPU))
	if len(d.Labels) > 0 {
		fmt.Printf("  labels     %s\n", formatLabels(d.Labels))
	}
	if len(d.Disks) == 0 {
		fmt.Printf("  disks      —\n")
		return
	}
	for _, disk := range d.Disks {
		fmt.Printf("  disk       %s  %s free / %s\n", disk.Mount, humanBytes(disk.FreeBytes), humanBytes(disk.TotalBytes))
	}
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func emptyDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func parseLabelList(raw string) map[string]string {
	out := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func formatLabels(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, " ")
}

func cpuShort(c *client.ComputeCPU) string {
	if c == nil {
		return "—"
	}
	return fmt.Sprintf("%d", c.Cores)
}

func memShort(m *client.ComputeMemory) string {
	if m == nil || m.TotalBytes == 0 {
		return "—"
	}
	return humanBytes(m.TotalBytes)
}

func gpuShort(gpus *[]client.ComputeGPU) string {
	if gpus == nil {
		return "unavailable"
	}
	if len(*gpus) == 0 {
		return "none"
	}
	g := (*gpus)[0]
	if g.VRAMBytes != nil {
		return fmt.Sprintf("%s %s (%s)", g.Vendor, g.Model, humanBytes(*g.VRAMBytes))
	}
	return strings.TrimSpace(g.Vendor + " " + g.Model)
}

func cmdJobs(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot jobs ls|run|show|artifacts|logs|cancel|wait ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	switch args[0] {
	case "list", "ls":
		list, err := cl.ListJobs(ctx, flags["device"])
		if err != nil {
			fatal(err)
		}
		if len(list) == 0 {
			fmt.Println("No jobs.")
			return
		}
		for _, j := range list {
			fmt.Printf("%s  %s  %s  cpu=%.0f ram=%dMB  %s  %s\n", j.Status, j.Image, j.DeviceName, j.Resources.CPU, j.Resources.MemoryMB, j.ID, j.DeviceID)
		}
	case "run", "create":
		if flags["image"] == "" {
			fatal(fmt.Errorf("usage: knot jobs run [--device ID] --image IMAGE [--cpu N] [--memory-mb N] [--gpu N|required] [--disk-mb N] [--require k=v] [--prefer k=v] [--wait]"))
		}
		var cmd []string
		if flags["command"] != "" {
			if err := json.Unmarshal([]byte(flags["command"]), &cmd); err != nil {
				fatal(fmt.Errorf("--command must be a JSON array, e.g. [\"python\",\"/input/main.py\"]"))
			}
		}
		var cpu float64
		var mem, disk, pids int64
		var gpu, timeout int
		fmt.Sscanf(flags["cpu"], "%f", &cpu)
		fmt.Sscanf(flags["memory-mb"], "%d", &mem)
		if strings.EqualFold(flags["gpu"], "required") {
			gpu = 1
		} else {
			fmt.Sscanf(flags["gpu"], "%d", &gpu)
		}
		fmt.Sscanf(flags["disk-mb"], "%d", &disk)
		fmt.Sscanf(flags["pids"], "%d", &pids)
		fmt.Sscanf(flags["timeout"], "%d", &timeout)
		job, err := cl.CreateJob(ctx, client.CreateJobRequest{
			DeviceID: flags["device"], Image: flags["image"], Command: cmd,
			Resources:      client.JobResources{CPU: cpu, MemoryMB: mem, GPU: gpu, Pids: pids, DiskMB: disk},
			TimeoutSeconds: timeout, InputPath: flags["input"], OutputPath: flags["output"],
			Require: parseLabelList(flags["require"]), Prefer: parseLabelList(flags["prefer"]),
		})
		if err != nil {
			fatal(err)
		}
		if flags["wait"] == "true" || flags["wait"] == "1" {
			waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			job, err = cl.WaitJob(waitCtx, job.ID, 200*time.Millisecond)
			if err != nil {
				fatal(err)
			}
		}
		fmt.Printf("%s  %s  %s  %s\n", job.Status, job.Image, job.ID, job.OutputPath)
	case "show":
		id := jobID(args, flags)
		job, err := cl.GetJob(ctx, id)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s\n", job.ID)
		fmt.Printf("  status     %s\n", job.Status)
		if job.Reason != "" {
			fmt.Printf("  reason     %s\n", job.Reason)
		}
		fmt.Printf("  image      %s\n", job.Image)
		fmt.Printf("  device     %s  %s\n", job.DeviceName, job.DeviceID)
		if job.Placement != "" {
			fmt.Printf("  placement  %s  attempts=%d/%d\n", job.Placement, job.Attempts, job.MaxRetries)
		}
		if len(job.Require) > 0 {
			fmt.Printf("  require    %s\n", formatLabels(job.Require))
		}
		if len(job.Prefer) > 0 {
			fmt.Printf("  prefer     %s\n", formatLabels(job.Prefer))
		}
		fmt.Printf("  resources  cpu=%.2f ram=%dMB gpu=%d disk=%dMB pids=%d\n", job.Resources.CPU, job.Resources.MemoryMB, job.Resources.GPU, job.Resources.DiskMB, job.Resources.Pids)
		fmt.Printf("  timeout    %ds\n", job.TimeoutSeconds)
		fmt.Printf("  input      %s\n", emptyDash(job.InputPath))
		fmt.Printf("  output     %s\n", emptyDash(job.OutputPath))
		if job.ExitCode != nil {
			fmt.Printf("  exit       %d\n", *job.ExitCode)
		}
		if job.Error != "" {
			fmt.Printf("  error      %s\n", job.Error)
		}
		if len(job.Artifacts) > 0 {
			fmt.Printf("  artifacts\n")
			for _, a := range job.Artifacts {
				fmt.Printf("    %s  %d  %s\n", a.Path, a.Size, a.SHA256)
			}
		}
	case "artifacts":
		id := jobID(args, flags)
		arts, err := cl.JobArtifacts(ctx, id)
		if err != nil {
			fatal(err)
		}
		if len(arts) == 0 {
			fmt.Println("No artifacts.")
			return
		}
		for _, a := range arts {
			fmt.Printf("%s  %d  %s  %s\n", a.Path, a.Size, a.SHA256, a.FileID)
		}
	case "logs":
		id := jobID(args, flags)
		logs, err := cl.JobLogs(ctx, id, 200)
		if err != nil {
			fatal(err)
		}
		for _, l := range logs {
			fmt.Printf("%s  %s\n", l.Stream, l.Message)
		}
	case "cancel":
		id := jobID(args, flags)
		job, err := cl.CancelJob(ctx, id)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("cancel requested  %s  %s\n", job.ID, job.Status)
	case "wait":
		id := jobID(args, flags)
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		job, err := cl.WaitJob(waitCtx, id, 200*time.Millisecond)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s  %s  %s\n", job.Status, job.Image, job.ID)
	default:
		fatal(fmt.Errorf("unknown jobs subcommand: %s", args[0]))
	}
}

func jobID(args []string, flags map[string]string) string {
	id := flags["id"]
	if id == "" && len(args) >= 2 {
		id = args[1]
	}
	if id == "" {
		fatal(fmt.Errorf("job id required"))
	}
	return id
}

func humanBytes(n uint64) string {
	const k = 1024
	if n < k {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(k), 0
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	for n/div >= k && exp < len(units)-1 {
		div *= k
		exp++
	}
	v := float64(n) / float64(div)
	if v < 10 {
		return fmt.Sprintf("%.1f %s", v, units[exp])
	}
	return fmt.Sprintf("%.0f %s", v, units[exp])
}

func cmdDevices() {
	cl := apiClient()
	list, err := cl.ListDevices(context.Background())
	if err != nil {
		fatal(err)
	}
	if len(list) == 0 {
		fmt.Println("No devices.")
		return
	}
	fmt.Printf("%-8s %-20s %-12s %-10s %-8s %s\n", "STATUS", "NAME", "OS", "ARCH", "AGENT", "ID")
	for _, d := range list {
		st := "offline"
		if d.Online {
			st = "online"
		}
		if d.RevokedAt != nil {
			st = "revoked"
		}
		ver := d.AgentVersion
		if ver == "" {
			ver = "-"
		}
		fmt.Printf("%-8s %-20s %-12s %-10s %-8s %s\n", st, d.Name, d.OS, d.Arch, ver, d.ID)
	}
}

func cmdDeviceShow(id string) {
	cl := apiClient()
	d, err := cl.GetDevice(context.Background(), id)
	if err != nil {
		fatal(err)
	}
	b, _ := json.MarshalIndent(d, "", "  ")
	fmt.Println(string(b))
}

func cmdRegToken() {
	cl := apiClient()
	name := ""
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--name" && i+1 < len(os.Args) {
			name = os.Args[i+1]
		}
	}
	tok, err := cl.CreateRegToken(context.Background(), name, 24)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("Registration token (shown once):\n%s\n", tok)
}

func cmdTransfer(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: knot transfer send|status|list ...")
		os.Exit(1)
	}
	cl := apiClient()
	switch args[0] {
	case "list":
		list, err := cl.ListTransfers(context.Background())
		if err != nil {
			fatal(err)
		}
		for _, t := range list {
			fmt.Printf("%-12s %-36s %s -> %s  %s (%d)\n", t.Status, t.ID, short(t.FromDeviceID), short(t.ToDeviceID), t.Filename, t.Size)
		}
	case "status":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: knot transfer status <id>"))
		}
		t, err := cl.GetTransfer(context.Background(), args[1])
		if err != nil {
			fatal(err)
		}
		b, _ := json.MarshalIndent(t, "", "  ")
		fmt.Println(string(b))
	case "send":
		flags := map[string]string{}
		for i := 1; i < len(args); i++ {
			if strings.HasPrefix(args[i], "--") && i+1 < len(args) {
				flags[strings.TrimPrefix(args[i], "--")] = args[i+1]
				i++
			}
		}
		from, to := flags["from"], flags["to"]
		file, sum, path := flags["file"], flags["sha256"], flags["path"]
		if path == "" {
			path = file
		}
		var size int64
		fmt.Sscanf(flags["size"], "%d", &size)
		if from == "" || to == "" || file == "" || sum == "" || size <= 0 {
			fatal(fmt.Errorf("usage: knot transfer send --from ID --to ID --file NAME --sha256 HEX --size N [--path REL]"))
		}
		t, err := cl.CreateTransfer(context.Background(), from, to, file, path, size, sum)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("transfer %s created (%s)\n", t.ID, t.Status)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		done, err := cl.WaitTransfer(ctx, t.ID, 200*time.Millisecond)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("status=%s error=%q\n", done.Status, done.Error)
		if done.Status != "completed" {
			os.Exit(1)
		}
	default:
		fatal(fmt.Errorf("unknown transfer subcommand: %s", args[0]))
	}
}

func cmdStorage(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: knot storage ls|stat|upload|download|mkdir|mv|rm|copy ...")
		os.Exit(1)
	}
	cl := apiClient()
	flags := map[string]string{}
	pos := []string{}
	for i := 1; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") && i+1 < len(args) {
			flags[strings.TrimPrefix(args[i], "--")] = args[i+1]
			i++
		} else {
			pos = append(pos, args[i])
		}
	}
	device := flags["device"]
	path := flags["path"]
	cmd := args[0]
	switch cmd {
	case "list", "ls":
		if device == "" {
			fatal(fmt.Errorf("usage: knot storage ls --device ID [--path REL]"))
		}
		ents, err := cl.StorageList(context.Background(), device, path)
		if err != nil {
			fatal(err)
		}
		for _, e := range ents {
			kind := "f"
			if e.IsDir {
				kind = "d"
			}
			fmt.Printf("%s %8d  %s\n", kind, e.Size, e.Path)
		}
	case "stat":
		if device == "" || path == "" {
			fatal(fmt.Errorf("usage: knot storage stat --device ID --path REL"))
		}
		st, err := cl.StorageStat(context.Background(), device, path)
		if err != nil {
			fatal(err)
		}
		b, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(b))
	case "mkdir":
		if device == "" || path == "" {
			fatal(fmt.Errorf("usage: knot storage mkdir --device ID --path REL"))
		}
		st, err := cl.StorageMkdir(context.Background(), device, path)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("mkdir %s\n", st.Path)
	case "delete", "rm":
		if device == "" || path == "" {
			fatal(fmt.Errorf("usage: knot storage rm --device ID --path REL"))
		}
		if err := cl.StorageDelete(context.Background(), device, path); err != nil {
			fatal(err)
		}
		fmt.Println("deleted")
	case "mv", "rename":
		from, to := flags["from"], flags["to"]
		if from == "" && len(pos) >= 1 {
			from = pos[0]
		}
		if to == "" && len(pos) >= 2 {
			to = pos[1]
		}
		if device == "" || from == "" || to == "" {
			fatal(fmt.Errorf("usage: knot storage mv --device ID --from REL --to REL"))
		}
		st, err := cl.StorageMove(context.Background(), device, from, to)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("moved -> %s\n", st.Path)
	case "copy", "cp":
		from, to := flags["from"], flags["to"]
		if from == "" && len(pos) >= 1 {
			from = pos[0]
		}
		if to == "" && len(pos) >= 2 {
			to = pos[1]
		}
		if device == "" || from == "" || to == "" {
			fatal(fmt.Errorf("usage: knot storage copy --device ID --from REL --to REL"))
		}
		st, err := cl.StorageCopy(context.Background(), device, from, to)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("copied -> %s file_id=%s\n", st.Path, st.FileID)
	case "upload":
		from := flags["from"]
		file := flags["file"]
		sum := flags["sha256"]
		resume := flags["resume"] == "1" || flags["resume"] == "true"
		var size int64
		fmt.Sscanf(flags["size"], "%d", &size)
		if device == "" || path == "" || from == "" || file == "" || sum == "" || size <= 0 {
			fatal(fmt.Errorf("usage: knot storage upload --device ID --path REL --from ID --file OUTBOX --sha256 HEX --size N [--resume true]"))
		}
		t, err := cl.StorageUploadOpts(context.Background(), device, path, from, file, size, sum, resume)
		if err != nil {
			fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		done, err := cl.WaitTransfer(ctx, t.ID, 200*time.Millisecond)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("status=%s file_id=%s transport=%s error=%q\n", done.Status, done.FileID, done.Path, done.Error)
		if done.Status != "completed" {
			os.Exit(1)
		}
	case "read", "download":
		to := flags["to"]
		if device == "" || path == "" || to == "" {
			fatal(fmt.Errorf("usage: knot storage download --device ID --path REL --to ID"))
		}
		t, err := cl.StorageRead(context.Background(), device, path, to)
		if err != nil {
			fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		done, err := cl.WaitTransfer(ctx, t.ID, 200*time.Millisecond)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("status=%s transport=%s error=%q\n", done.Status, done.Path, done.Error)
		if done.Status != "completed" {
			os.Exit(1)
		}
	default:
		fatal(fmt.Errorf("unknown storage subcommand: %s", cmd))
	}
}

func cmdSync(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot sync create|list|show|run|cancel|rm|wait|conflicts|resolve|flush ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	switch args[0] {
	case "list", "ls":
		jobs, err := cl.ListSyncJobs(ctx)
		if err != nil {
			fatal(err)
		}
		for _, j := range jobs {
			fmt.Printf("%s  %-10s  %s → %s  %s/%s  %d/%d files\n",
				short(j.ID), j.Status, short(j.SourceDeviceID), short(j.DestDeviceID),
				j.SourcePath, j.DestPath, j.FilesDone, j.FilesTotal)
		}
	case "show":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: knot sync show <id>"))
		}
		j, err := cl.GetSyncJob(ctx, args[1])
		if err != nil {
			fatal(err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(j)
	case "create":
		fs := map[string]string{}
		parseFlags(args[1:], fs)
		from, to := fs["from"], fs["to"]
		fromPath, toPath := fs["from-path"], fs["to-path"]
		mode := fs["mode"]
		if mode == "" {
			mode = "one_way"
		}
		if from == "" || to == "" || fromPath == "" || toPath == "" {
			fatal(fmt.Errorf("usage: knot sync create --from ID --from-path REL --to ID --to-path REL [--mode one_way|two_way] [--name NAME]"))
		}
		j, err := cl.CreateSyncJob(ctx, client.CreateSyncJobRequest{
			Name:           fs["name"],
			Mode:           mode,
			SourceDeviceID: from,
			SourcePath:     fromPath,
			DestDeviceID:   to,
			DestPath:       toPath,
		})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("created %s mode=%s (%s)\n", j.ID, j.Mode, j.Name)
	case "conflicts":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: knot sync conflicts <job-id>"))
		}
		list, err := cl.ListSyncConflicts(ctx, args[1])
		if err != nil {
			fatal(err)
		}
		for _, c := range list {
			fmt.Printf("%s  %-8s  %s  a=%s b=%s\n", short(c.ID), c.Status, c.RelPath, short(c.ASHA256), short(c.BSHA256))
		}
	case "resolve":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: knot sync resolve <conflict-id> --resolution keep_a|keep_b|keep_both\n       knot sync resolve --batch <id,id,...> --resolution keep_a|keep_b|keep_both"))
		}
		fs := map[string]string{}
		parseFlags(args[1:], fs)
		res := fs["resolution"]
		if res == "" {
			fatal(fmt.Errorf("--resolution required"))
		}
		if batch := fs["batch"]; batch != "" {
			ids := strings.Split(batch, ",")
			var clean []string
			for _, id := range ids {
				id = strings.TrimSpace(id)
				if id != "" {
					clean = append(clean, id)
				}
			}
			resolved, errs, err := cl.BatchResolveSyncConflicts(ctx, clean, res)
			if err != nil {
				fatal(err)
			}
			fmt.Printf("batch resolved %d\n", len(resolved))
			for _, e := range errs {
				fmt.Printf("  error: %s\n", e)
			}
			break
		}
		c, err := cl.ResolveSyncConflict(ctx, args[1], res)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("resolved %s → %s\n", c.ID, c.Resolution)
	case "run":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: knot sync run <id>"))
		}
		j, err := cl.RunSyncJob(ctx, args[1])
		if err != nil {
			fatal(err)
		}
		fmt.Printf("running %s status=%s\n", j.ID, j.Status)
	case "cancel":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: knot sync cancel <id>"))
		}
		j, err := cl.CancelSyncJob(ctx, args[1])
		if err != nil {
			fatal(err)
		}
		fmt.Printf("cancel %s status=%s\n", j.ID, j.Status)
	case "rm", "delete":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: knot sync rm <id>"))
		}
		if err := cl.DeleteSyncJob(ctx, args[1]); err != nil {
			fatal(err)
		}
		fmt.Println("deleted")
	case "wait":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: knot sync wait <id>"))
		}
		wctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		j, err := cl.WaitSyncJob(wctx, args[1], 200*time.Millisecond)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("status=%s files=%d/%d error=%q\n", j.Status, j.FilesDone, j.FilesTotal, j.LastError)
		if j.Status != "completed" && j.Status != "completed_with_conflicts" {
			os.Exit(1)
		}
	case "flush":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: knot sync flush <device-id>"))
		}
		res, err := cl.FlushSync(ctx, args[1])
		if err != nil {
			fatal(err)
		}
		fmt.Printf("flush device=%s jobs=%d conflicts=%d\n", short(res.DeviceID), len(res.JobIDs), len(res.ConflictPaths))
		for _, e := range res.Errors {
			fmt.Printf("  error: %s\n", e)
		}
		for _, p := range res.ConflictPaths {
			fmt.Printf("  conflict: %s\n", p)
		}
	default:
		fatal(fmt.Errorf("unknown sync subcommand: %s", args[0]))
	}
}

func cmdFiles(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot files search|reindex ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	switch args[0] {
	case "search":
		q := client.FileSearchQuery{
			Query:          flags["q"],
			DeviceID:       flags["device"],
			Folder:         flags["folder"],
			Type:           flags["type"],
			ModifiedAfter:  flags["modified-after"],
			ModifiedBefore: flags["modified-before"],
		}
		fmt.Sscanf(flags["min-size"], "%d", &q.MinSize)
		fmt.Sscanf(flags["max-size"], "%d", &q.MaxSize)
		hits, err := cl.FilesSearch(ctx, q)
		if err != nil {
			fatal(err)
		}
		for _, h := range hits {
			kind := "f"
			if h.IsDirectory {
				kind = "d"
			}
			node := h.DeviceName
			if node == "" {
				node = short(h.DeviceID)
			}
			fmt.Printf("%s  %s  /%s  %d\n", kind, node, h.Path, h.Size)
		}
	case "reindex":
		res, err := cl.FilesReindex(ctx, flags["device"])
		if err != nil {
			fatal(err)
		}
		fmt.Printf("indexed=%d devices=%d skipped=%d\n", res.Entries, len(res.DeviceIDs), len(res.Skipped))
		for _, e := range res.Errors {
			fmt.Printf("  error: %s\n", e)
		}
	default:
		fatal(fmt.Errorf("unknown files subcommand: %s", args[0]))
	}
}

func cmdServices(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot services ls|tree|add|show|health|rm ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	switch args[0] {
	case "list", "ls":
		list, err := cl.ListServices(ctx, flags["device"])
		if err != nil {
			fatal(err)
		}
		for _, svc := range list {
			node := svc.DeviceName
			if node == "" {
				node = short(svc.DeviceID)
			}
			fmt.Printf("%s  %s  %s  %s  %s\n", node, svc.Name, svc.Kind, svc.Listen, svc.Status)
		}
	case "tree":
		nodes, err := cl.ServicesTree(ctx)
		if err != nil {
			fatal(err)
		}
		for _, n := range nodes {
			state := "offline"
			if n.Online {
				state = "online"
			}
			fmt.Printf("%s  (%s)\n", n.DeviceName, state)
			if len(n.Services) == 0 {
				fmt.Printf("  (no services)\n")
				continue
			}
			for i, svc := range n.Services {
				branch := "├──"
				if i == len(n.Services)-1 {
					branch = "└──"
				}
				hosts := ""
				if len(svc.Hostnames) > 0 {
					hosts = "  " + strings.Join(svc.Hostnames, ",")
				}
				fmt.Printf("  %s %s  %s%s\n", branch, svc.Name, svc.Listen, hosts)
			}
		}
	case "add", "register", "create":
		var port int
		fmt.Sscanf(flags["port"], "%d", &port)
		if flags["device"] == "" || flags["name"] == "" || port == 0 {
			fatal(fmt.Errorf("usage: knot services add --device ID --name NAME --port N [--kind web] [--protocol http] [--bind 127.0.0.1]"))
		}
		svc, err := cl.RegisterService(ctx, client.RegisterServiceRequest{
			DeviceID: flags["device"],
			Name:     flags["name"],
			Kind:     flags["kind"],
			Protocol: flags["protocol"],
			Port:     port,
			Bind:     flags["bind"],
		})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("registered %s on %s %s\n", svc.Name, svc.DeviceName, svc.Listen)
	case "show":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: knot services show <id>"))
		}
		svc, err := cl.GetService(ctx, args[1])
		if err != nil {
			fatal(err)
		}
		b, _ := json.MarshalIndent(svc, "", "  ")
		fmt.Println(string(b))
	case "health":
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: knot services health <id>"))
		}
		svc, err := cl.ServiceHealth(ctx, args[1])
		if err != nil {
			fatal(err)
		}
		b, _ := json.MarshalIndent(svc, "", "  ")
		fmt.Println(string(b))
	case "rm", "delete":
		id := flags["id"]
		if id == "" && len(args) >= 2 {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot services rm <id>"))
		}
		if err := cl.DeleteService(ctx, id); err != nil {
			fatal(err)
		}
		fmt.Println("deleted")
	default:
		fatal(fmt.Errorf("unknown services subcommand: %s", args[0]))
	}
}

func cmdRoutes(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot routes ls|add|rm ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	switch args[0] {
	case "list", "ls":
		list, err := cl.ListRoutes(ctx)
		if err != nil {
			fatal(err)
		}
		for _, rt := range list {
			edgeName := rt.EdgeDeviceName
			if edgeName == "" {
				edgeName = "control-plane"
			}
			fmt.Printf("%s  →  %s  %s  edge:%s\n", rt.Hostname, rt.ServiceName, rt.Listen, edgeName)
		}
	case "add", "create":
		host := flags["host"]
		if host == "" {
			host = flags["hostname"]
		}
		svc := flags["service"]
		if svc == "" {
			svc = flags["service-id"]
		}
		if host == "" || svc == "" {
			fatal(fmt.Errorf("usage: knot routes add --host HOST --service ID [--edge DEVICE]"))
		}
		rt, err := cl.CreateRoute(ctx, client.CreateRouteRequest{
			Hostname:     host,
			ServiceID:    svc,
			EdgeDeviceID: flags["edge"],
			TLSMode:      flags["tls-mode"],
		})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("routed %s → %s %s\n", rt.Hostname, rt.ServiceName, rt.Listen)
	case "rm", "delete":
		id := flags["id"]
		if id == "" && len(args) >= 2 {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot routes rm <id>"))
		}
		if err := cl.DeleteRoute(ctx, id); err != nil {
			fatal(err)
		}
		fmt.Println("deleted")
	default:
		fatal(fmt.Errorf("unknown routes subcommand: %s", args[0]))
	}
}

func cmdEnv(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot env ls|create|show ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	switch args[0] {
	case "list", "ls":
		list, err := cl.ListEnvironments(ctx, flags["project"])
		if err != nil {
			fatal(err)
		}
		if len(list) == 0 {
			fmt.Println("No environments.")
			return
		}
		for _, e := range list {
			proj := e.Project
			if proj == "" {
				proj = "-"
			}
			fmt.Printf("%s  %s/%s  vars=%d secrets=%d  %s\n", e.ID, proj, e.Name, len(e.Vars), len(e.Secrets), e.ID)
		}
	case "create":
		name := flags["name"]
		if name == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			name = args[1]
		}
		if name == "" {
			fatal(fmt.Errorf("usage: knot env create NAME [--project NAME] [--var K=V] [--secret KEY=SECRET]"))
		}
		env, err := cl.CreateEnvironment(ctx, client.CreateEnvironmentRequest{
			Project: flags["project"], Name: name,
			Vars:    parseLabelList(flags["var"]),
			Secrets: parseLabelList(flags["secret"]),
		})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("created %s  %s/%s\n", env.ID, env.Project, env.Name)
	case "show":
		id := flags["id"]
		if id == "" && len(args) >= 2 {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot env show <id>"))
		}
		e, err := cl.GetEnvironment(ctx, id)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s  %s/%s\n", e.ID, e.Project, e.Name)
		for k, v := range e.Vars {
			fmt.Printf("  var     %s=%s\n", k, v)
		}
		for _, s := range e.Secrets {
			fmt.Printf("  secret  %s → %s v%d\n", s.Key, s.Name, s.Version)
		}
	default:
		fatal(fmt.Errorf("unknown env subcommand: %s", args[0]))
	}
}

func cmdSecret(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot secret ls|create|show|rotate ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	switch args[0] {
	case "list", "ls":
		list, err := cl.ListSecrets(ctx)
		if err != nil {
			fatal(err)
		}
		if len(list) == 0 {
			fmt.Println("No secrets.")
			return
		}
		for _, s := range list {
			fmt.Printf("%s  %s  v%d\n", s.ID, s.Name, s.Version)
		}
	case "create":
		name := flags["name"]
		if name == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			name = args[1]
		}
		if name == "" {
			fatal(fmt.Errorf("usage: knot secret create NAME [--value VALUE]"))
		}
		val := flags["value"]
		if val == "" {
			fmt.Fprint(os.Stderr, "value: ")
			b, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				fatal(err)
			}
			val = string(b)
		}
		sec, err := cl.CreateSecret(ctx, name, val)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("created %s  %s  v%d\n", sec.ID, sec.Name, sec.Version)
	case "show":
		id := flags["id"]
		if id == "" && len(args) >= 2 {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot secret show <id-or-name>"))
		}
		sec, err := cl.GetSecret(ctx, id)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s  %s  v%d\n", sec.ID, sec.Name, sec.Version)
	case "rotate":
		id := flags["id"]
		if id == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot secret rotate <id-or-name> [--value VALUE]"))
		}
		val := flags["value"]
		if val == "" {
			fmt.Fprint(os.Stderr, "value: ")
			b, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				fatal(err)
			}
			val = string(b)
		}
		sec, err := cl.RotateSecret(ctx, id, val)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("rotated %s  %s  v%d\n", sec.ID, sec.Name, sec.Version)
	default:
		fatal(fmt.Errorf("unknown secret subcommand: %s", args[0]))
	}
}

func cmdSource(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot source add|ls|show ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	switch args[0] {
	case "list", "ls":
		list, err := cl.ListSources(ctx)
		if err != nil {
			fatal(err)
		}
		if len(list) == 0 {
			fmt.Println("No sources.")
			return
		}
		for _, s := range list {
			fmt.Printf("%s  %s  %s  %s\n", s.ID, s.Name, s.URL, s.Branch)
		}
	case "add", "create":
		if flags["url"] == "" {
			fatal(fmt.Errorf("usage: knot source add --url repo.git [--branch main] [--secret ID]"))
		}
		src, err := cl.CreateSource(ctx, client.CreateSourceRequest{
			URL: flags["url"], Branch: flags["branch"], GitTag: flags["tag"],
			Name: flags["name"], Revision: flags["revision"], CredentialSecretID: flags["secret"],
		})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s  %s  %s\n", src.ID, src.URL, src.Branch)
	case "show":
		id := flags["id"]
		if id == "" && len(args) >= 2 {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot source show <id>"))
		}
		src, err := cl.GetSource(ctx, id)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s\n  type     %s\n  name     %s\n  url      %s\n  branch   %s\n  revision %s\n",
			src.ID, src.Type, src.Name, src.URL, src.Branch, src.Revision)
		if src.CredentialSecretID != "" {
			fmt.Printf("  secret   %s\n", src.CredentialSecretID)
		}
	default:
		fatal(fmt.Errorf("unknown source subcommand: %s", args[0]))
	}
}

func cmdBuild(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot build run|ls|show|logs ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	switch args[0] {
	case "list", "ls":
		list, err := cl.ListBuilds(ctx, flags["source"], flags["device"])
		if err != nil {
			fatal(err)
		}
		if len(list) == 0 {
			fmt.Println("No builds.")
			return
		}
		for _, b := range list {
			img := b.Image
			if img == "" {
				img = b.Tag
			}
			fmt.Printf("%s  %s  %s  %s\n", b.Status, img, b.ID, b.DeviceName)
		}
	case "run", "create":
		sourceID := flags["source"]
		if sourceID == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			sourceID = args[1]
		}
		if sourceID == "" || flags["device"] == "" || flags["tag"] == "" {
			fatal(fmt.Errorf("usage: knot build run <source-id> --device ID --tag IMAGE"))
		}
		b, err := cl.CreateBuild(ctx, client.CreateBuildRequest{
			SourceID: sourceID, DeviceID: flags["device"], Tag: flags["tag"],
			Dockerfile: flags["dockerfile"], Context: flags["context"],
			RegistrySecretID: flags["registry-secret"],
		})
		if err != nil {
			fatal(err)
		}
		if flags["wait"] == "true" || flags["wait"] == "1" {
			waitCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
			defer cancel()
			b, err = cl.WaitBuild(waitCtx, b.ID, 200*time.Millisecond)
			if err != nil {
				fatal(err)
			}
		}
		fmt.Printf("%s  %s  %s\n", b.Status, b.Tag, b.ID)
	case "show":
		id := flags["id"]
		if id == "" && len(args) >= 2 {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot build show <id>"))
		}
		b, err := cl.GetBuild(ctx, id)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s\n  status   %s\n  tag      %s\n  image    %s\n  revision %s\n  device   %s\n  error    %s\n",
			b.ID, b.Status, b.Tag, b.Image, b.Revision, b.DeviceName, b.Error)
	case "logs":
		id := flags["id"]
		if id == "" && len(args) >= 2 {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot build logs <id>"))
		}
		logs, err := cl.BuildLogs(ctx, id, 200)
		if err != nil {
			fatal(err)
		}
		for _, l := range logs {
			fmt.Printf("%s  %s\n", l.Stream, l.Message)
		}
	default:
		fatal(fmt.Errorf("unknown build subcommand: %s", args[0]))
	}
}

func cmdRelease(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot release ls|create|deploy|show|logs|rollback ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	idFrom := func() string {
		id := flags["id"]
		if id == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("release id required"))
		}
		return id
	}
	switch args[0] {
	case "list", "ls":
		list, err := cl.ListReleases(ctx, flags["service"])
		if err != nil {
			fatal(err)
		}
		if len(list) == 0 {
			fmt.Println("No releases.")
			return
		}
		for _, r := range list {
			cur := ""
			if r.Current {
				cur = "  current"
			}
			fmt.Printf("#%d  %s  %s  %s  %s%s\n", r.Number, r.Status, r.Service, r.Image, r.ID, cur)
		}
	case "create":
		svc := flags["service"]
		if svc == "" {
			svc = flags["name"]
		}
		if svc == "" || (flags["image"] == "" && flags["build"] == "") {
			fatal(fmt.Errorf("usage: knot release create --service NAME --image IMAGE [--environment NAME] [--device ID] [--port N]"))
		}
		port := 0
		fmt.Sscanf(flags["port"], "%d", &port)
		rel, err := cl.CreateRelease(ctx, client.CreateReleaseRequest{
			Service: svc, Image: flags["image"], Environment: flags["environment"],
			Project: flags["project"], DeviceID: flags["device"], Port: port,
			HealthPath: flags["health"], BuildID: flags["build"], JobID: flags["job"],
		})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("#%d  %s  %s  %s\n", rel.Number, rel.Status, rel.Image, rel.ID)
	case "deploy":
		id := idFrom()
		port := 0
		fmt.Sscanf(flags["port"], "%d", &port)
		rel, err := cl.DeployRelease(ctx, id, flags["device"], port)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("#%d  %s  %s  %s\n", rel.Number, rel.Status, rel.Image, rel.ID)
	case "show":
		rel, err := cl.GetRelease(ctx, idFrom())
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s\n  number   #%d\n  status   %s\n  service  %s\n  image    %s\n  env      %s\n  current  %v\n  error    %s\n",
			rel.ID, rel.Number, rel.Status, rel.Service, rel.Image, rel.Environment, rel.Current, rel.Error)
	case "logs":
		logs, err := cl.ReleaseLogs(ctx, idFrom(), 200)
		if err != nil {
			fatal(err)
		}
		for _, l := range logs {
			fmt.Printf("%s  %s  %s\n", l.Source, l.Stream, l.Message)
		}
	case "rollback":
		rel, err := cl.RollbackRelease(ctx, idFrom())
		if err != nil {
			fatal(err)
		}
		fmt.Printf("#%d  %s  %s\n", rel.Number, rel.Status, rel.Image)
	default:
		fatal(fmt.Errorf("unknown release subcommand: %s", args[0]))
	}
}

func cmdTraffic(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot traffic show|switch|rollback ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	routeFrom := func() string {
		id := flags["route"]
		if id == "" {
			id = flags["id"]
		}
		if id == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("route id or hostname required"))
		}
		return id
	}
	switch args[0] {
	case "show", "status", "ls":
		st, err := cl.GetRouteTraffic(ctx, routeFrom())
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s  active=%s  prev=%s\n", st.Hostname, st.ActiveReleaseID, st.PrevReleaseID)
		for _, t := range st.Targets {
			fmt.Printf("  #%d  w=%d  %s  %s  %s\n", t.Number, t.Weight, t.Status, t.Image, t.ReleaseID)
		}
	case "switch":
		rel := flags["release"]
		if rel == "" {
			rel = flags["release-id"]
		}
		if rel == "" {
			fatal(fmt.Errorf("usage: knot traffic switch --route HOST --release ID"))
		}
		weight := 100
		fmt.Sscanf(flags["weight"], "%d", &weight)
		st, err := cl.SwitchRouteTraffic(ctx, routeFrom(), rel, weight)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("switched %s → %s\n", st.Hostname, st.ActiveReleaseID)
	case "rollback":
		st, err := cl.RollbackRouteTraffic(ctx, routeFrom())
		if err != nil {
			fatal(err)
		}
		fmt.Printf("rolled back %s → %s\n", st.Hostname, st.ActiveReleaseID)
	default:
		fatal(fmt.Errorf("unknown traffic subcommand: %s", args[0]))
	}
}

func cmdLogs(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot logs list|service|follow|release ..."))
	}
	cl := apiClient()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	q := client.ListLogsQuery{
		Service:   flags["service"],
		ReleaseID: flags["release"],
		Source:    flags["source"],
		TraceID:   flags["trace"],
		Level:     flags["level"],
		Q:         flags["q"],
		After:     flags["after"],
	}
	if q.ReleaseID == "" {
		q.ReleaseID = flags["release-id"]
	}
	if q.TraceID == "" {
		q.TraceID = flags["trace-id"]
	}
	printLogs := func(list []client.OpsLog) {
		for _, e := range list {
			ts := e.Timestamp
			if t, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil {
				ts = t.Format("15:04:05")
			} else if t, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
				ts = t.Format("15:04:05")
			}
			fmt.Printf("[%s] %s  %s\n", ts, e.Source, e.Message)
		}
	}
	switch args[0] {
	case "list", "ls":
		list, err := cl.ListLogs(context.Background(), q)
		if err != nil {
			fatal(err)
		}
		printLogs(list)
	case "service":
		name := flags["service"]
		if name == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			name = args[1]
		}
		if name == "" {
			fatal(fmt.Errorf("usage: knot logs service <name>"))
		}
		q.Service = name
		list, err := cl.ListLogs(context.Background(), q)
		if err != nil {
			fatal(err)
		}
		printLogs(list)
	case "release":
		id := flags["release"]
		if id == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot logs release <id>"))
		}
		q.ReleaseID = id
		list, err := cl.ListLogs(context.Background(), q)
		if err != nil {
			fatal(err)
		}
		printLogs(list)
	case "follow":
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		after := q.After
		if after == "" {
			snap, err := cl.ListLogs(ctx, q)
			if err != nil {
				fatal(err)
			}
			printLogs(snap)
			if len(snap) > 0 {
				after = snap[len(snap)-1].ID
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(400 * time.Millisecond):
				fq := q
				fq.After = after
				list, err := cl.ListLogs(ctx, fq)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					fatal(err)
				}
				printLogs(list)
				if len(list) > 0 {
					after = list[len(list)-1].ID
				}
			}
		}
	default:
		fatal(fmt.Errorf("unknown logs subcommand: %s", args[0]))
	}
}

func cmdOps(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot ops context <service>"))
	}
	cl := apiClient()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	switch args[0] {
	case "context", "show", "status":
		name := flags["service"]
		if name == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			name = args[1]
		}
		if name == "" {
			fatal(fmt.Errorf("usage: knot ops context <service>"))
		}
		view, err := cl.OpsContext(context.Background(), name, flags["device"])
		if err != nil {
			fatal(err)
		}
		fmt.Println(view.Summary)
	default:
		fatal(fmt.Errorf("unknown ops subcommand: %s", args[0]))
	}
}

func cmdWorkflow(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot workflow ls|run|show ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	switch args[0] {
	case "list", "ls":
		list, err := cl.ListWorkflows(ctx)
		if err != nil {
			fatal(err)
		}
		fmt.Println("catalog:")
		for _, c := range list.Catalog {
			kind := "read-only"
			if c.Mutating {
				kind = "mutating"
			}
			fmt.Printf("  %s  %s  (%s)\n", c.Name, c.Title, kind)
		}
		fmt.Println("runs:")
		for _, w := range list.Workflows {
			fmt.Printf("  %s  %s  %s  trace=%s\n", short(w.ID), w.Name, w.Status, w.TraceID)
		}
	case "run":
		name := flags["name"]
		if name == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			name = args[1]
		}
		if name == "" {
			fatal(fmt.Errorf("usage: knot workflow run diagnose-service|deploy-release|restore-backup ..."))
		}
		port := 0
		fmt.Sscanf(flags["port"], "%d", &port)
		wf, err := cl.RunWorkflow(ctx, client.RunWorkflowRequest{
			Name: name, Service: flags["service"], DeviceID: flags["device"],
			Image: flags["image"], BuildID: flags["build"], Port: port, Hostname: flags["hostname"],
			Environment: flags["environment"], Query: flags["query"], Path: flags["path"],
			FromDeviceID: flags["from"], ToDeviceID: flags["to"], ToPath: flags["to-path"],
			JobImage: flags["job-image"],
		})
		if err != nil {
			fatal(err)
		}
		printWorkflow(wf)
	case "show", "status":
		id := flags["id"]
		if id == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot workflow show <id>"))
		}
		wf, err := cl.GetWorkflow(ctx, id)
		if err != nil {
			fatal(err)
		}
		printWorkflow(wf)
	default:
		fatal(fmt.Errorf("unknown workflow subcommand: %s", args[0]))
	}
}

func printWorkflow(wf *client.Workflow) {
	fmt.Printf("%s  %s  %s  trace=%s  actor=%s\n", wf.ID, wf.Name, wf.Status, wf.TraceID, wf.Actor)
	if wf.Error != "" {
		fmt.Printf("  error %s\n", wf.Error)
	}
	if cause, ok := wf.Result["cause"]; ok {
		fmt.Printf("  cause %v\n", cause)
	}
	if traffic, ok := wf.Result["traffic"]; ok {
		fmt.Printf("  traffic %v\n", traffic)
	}
	if rec, ok := wf.Result["recommendation"]; ok {
		fmt.Printf("  rec %v\n", rec)
	}
	for _, st := range wf.Steps {
		fmt.Printf("  %d. %s  %s", st.Seq, st.Name, st.Status)
		if st.Error != "" {
			fmt.Printf("  %s", st.Error)
		}
		fmt.Println()
	}
}

func cmdPlan(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot plan list|show|create|approve|execute|cancel ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	switch args[0] {
	case "list", "ls":
		list, err := cl.ListPlans(ctx)
		if err != nil {
			fatal(err)
		}
		fmt.Println("catalog:")
		for _, c := range list.Catalog {
			appr := "auto"
			if c.Approval {
				appr = "approval"
			}
			fmt.Printf("  %s  %s  risk=%s  (%s)\n", c.Name, c.Title, c.Risk, appr)
		}
		fmt.Println("plans:")
		for _, p := range list.Plans {
			fmt.Printf("  %s  %s  %s  risk=%s  expires=%s\n", short(p.ID), p.Name, p.Status, p.RiskLevel, p.ExpiresAt)
		}
	case "create":
		name := flags["name"]
		if name == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			name = args[1]
		}
		port := 0
		fmt.Sscanf(flags["port"], "%d", &port)
		p, err := cl.CreatePlan(ctx, client.CreatePlanRequest{
			Name: name, Intent: flags["intent"], Service: flags["service"], DeviceID: flags["device"],
			Image: flags["image"], BuildID: flags["build"], Port: port, Hostname: flags["hostname"],
			Environment: flags["environment"], ExpiresIn: flags["ttl"],
		})
		if err != nil {
			fatal(err)
		}
		printPlan(p)
	case "show", "status":
		id := planIDArg(args, flags)
		p, err := cl.GetPlan(ctx, id)
		if err != nil {
			fatal(err)
		}
		printPlan(p)
	case "approve":
		id := planIDArg(args, flags)
		p, err := cl.ApprovePlan(ctx, id)
		if err != nil {
			fatal(err)
		}
		printPlan(p)
	case "execute":
		id := planIDArg(args, flags)
		p, err := cl.ExecutePlan(ctx, id)
		if err != nil {
			fatal(err)
		}
		printPlan(p)
	case "cancel":
		id := planIDArg(args, flags)
		p, err := cl.CancelPlan(ctx, id)
		if err != nil {
			fatal(err)
		}
		printPlan(p)
	default:
		fatal(fmt.Errorf("unknown plan subcommand: %s", args[0]))
	}
}

func planIDArg(args []string, flags map[string]string) string {
	id := flags["id"]
	if id == "" && len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
		id = args[1]
	}
	if id == "" {
		fatal(fmt.Errorf("usage: knot plan show|approve|execute|cancel <id>"))
	}
	return id
}

func printPlan(p *client.Plan) {
	fmt.Printf("%s  %s  %s  risk=%s  trace=%s\n", p.ID, p.Name, p.Status, p.RiskLevel, p.TraceID)
	if p.Intent != "" {
		fmt.Printf("  intent %s\n", p.Intent)
	}
	if p.Error != "" {
		fmt.Printf("  error %s\n", p.Error)
	}
	for _, st := range p.Steps {
		mark := "·"
		if st.Status == "succeeded" {
			mark = "✓"
		} else if st.Status == "failed" || st.Status == "denied" {
			mark = "✗"
		}
		fmt.Printf("  %s %d. %s  %s\n", mark, st.Seq, st.Name, st.Status)
	}
}

func cmdAI(args []string) {
	if len(args) < 1 || args[0] != "session" && args[0] != "sessions" {
		fatal(fmt.Errorf("usage: knot ai session create|ls|revoke|show ..."))
	}
	rest := args[1:]
	if len(rest) < 1 {
		fatal(fmt.Errorf("usage: knot ai session create|ls|revoke|show ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	switch rest[0] {
	case "list", "ls":
		list, err := cl.ListAISessions(ctx)
		if err != nil {
			fatal(err)
		}
		for _, s := range list {
			fmt.Printf("%s  %s  %s  expires=%s  %s\n", short(s.ID), s.Name, s.Status, s.ExpiresAt, strings.Join(s.Scopes, ","))
		}
	case "create":
		scopes, ttl, name := parseAISessionFlags(rest[1:])
		if len(scopes) == 0 {
			fatal(fmt.Errorf("usage: knot ai session create --scope logs.read [--scope release.read] [--ttl 30m] [--name NAME]"))
		}
		sess, err := cl.CreateAISession(ctx, client.CreateAISessionRequest{Name: name, Scopes: scopes, ExpiresIn: ttl})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s  expires %s\n", sess.ID, sess.ExpiresAt)
		if sess.Token != "" {
			fmt.Printf("token %s\n", sess.Token)
		}
	case "revoke", "rm":
		id := ""
		if len(rest) >= 2 {
			id = rest[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot ai session revoke <id>"))
		}
		if err := cl.RevokeAISession(ctx, id); err != nil {
			fatal(err)
		}
		fmt.Println("revoked")
	case "show":
		id := ""
		if len(rest) >= 2 && !strings.HasPrefix(rest[1], "--") {
			id = rest[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot ai session show <id>"))
		}
		sess, err := cl.GetAISession(ctx, id)
		if err != nil {
			fatal(err)
		}
		printAISession(sess)
	case "current", "whoami":
		sess, err := cl.CurrentAISession(ctx)
		if err != nil {
			fatal(err)
		}
		printAISession(sess)
	default:
		fatal(fmt.Errorf("unknown ai session subcommand: %s", rest[0]))
	}
}

func cmdAudit(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot audit search|ai|trace ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	switch args[0] {
	case "search":
		q, asJSON := parseAuditFlags(args[1:])
		events, err := cl.SearchAudit(ctx, q)
		if err != nil {
			fatal(err)
		}
		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(events)
			return
		}
		for _, e := range events {
			when := e.Time
			if when == "" {
				when = e.CreatedAt
			}
			fmt.Printf("%s  %s  %s", when, e.ActorType, e.Actor)
			if e.Parent != "" {
				fmt.Printf("  parent=%s", e.Parent)
			}
			fmt.Printf("  %s  %s  %s", e.Action, e.Target, e.Result)
			if e.TraceID != "" {
				fmt.Printf("  trace=%s", e.TraceID)
			}
			fmt.Println()
		}
	case "ai":
		q, _ := parseAuditFlags(args[1:])
		list, err := cl.AIActivity(ctx, q)
		if err != nil {
			fatal(err)
		}
		for _, a := range list {
			fmt.Println(a.Time)
			fmt.Println(a.Actor)
			if a.Ran != "" {
				fmt.Printf("Ran:\n%s\n", a.Ran)
			} else if a.Action != "" {
				fmt.Printf("Action:\n%s\n", a.Action)
			}
			if a.Service != "" {
				fmt.Printf("Service:\n%s\n", a.Service)
			} else if a.Target != "" {
				fmt.Printf("Target:\n%s\n", a.Target)
			}
			if len(a.Steps) > 0 {
				fmt.Println("Steps:")
				for _, st := range a.Steps {
					mark := "✗"
					if st.OK {
						mark = "✓"
					}
					fmt.Printf("%s %s\n", mark, st.Name)
				}
			}
			if a.Result != "" {
				fmt.Printf("Result:\n%s\n", a.Result)
			}
			fmt.Println()
		}
	case "trace":
		id := ""
		if len(args) >= 2 {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot audit trace <trace_id>"))
		}
		events, err := cl.AuditTrace(ctx, id)
		if err != nil {
			fatal(err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(events)
	default:
		fatal(fmt.Errorf("unknown audit subcommand: %s", args[0]))
	}
}

func parseAuditFlags(args []string) (client.AuditQuery, bool) {
	var q client.AuditQuery
	asJSON := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--json" {
			asJSON = true
			continue
		}
		if !strings.HasPrefix(a, "--") {
			continue
		}
		key := strings.TrimPrefix(a, "--")
		val := ""
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			val = args[i+1]
			i++
		}
		switch key {
		case "action":
			q.Action = val
		case "actor-type", "actor_type":
			q.ActorType = val
		case "session", "ai-session", "ai_session_id":
			q.AISessionID = val
		case "workflow", "workflow_id":
			q.WorkflowID = val
		case "trace", "trace_id":
			q.TraceID = val
		case "mcp", "mcp-client", "mcp_client":
			q.MCPClient = val
		case "q", "query":
			q.Q = val
		case "limit":
			fmt.Sscanf(val, "%d", &q.Limit)
		}
	}
	return q, asJSON
}

func parseAISessionFlags(args []string) (scopes []string, ttl, name string) {
	ttl = "30m"
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			continue
		}
		key := strings.TrimPrefix(a, "--")
		val := ""
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			val = args[i+1]
			i++
		}
		switch key {
		case "scope", "scopes":
			for _, p := range strings.Split(val, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					scopes = append(scopes, p)
				}
			}
		case "ttl":
			ttl = val
		case "name":
			name = val
		}
	}
	return scopes, ttl, name
}

func printAISession(sess *client.AISession) {
	fmt.Printf("Session: %s\nActor: %s\nCreated: %s\nScopes: %s\nExpires: %s\nStatus: %s\n",
		sess.Name, sess.Actor, sess.CreatedAt, strings.Join(sess.Scopes, ","), sess.ExpiresAt, sess.Status)
}

func cmdDeploy(args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: knot deploy ls|create|show|stop|restart|rollback|logs ..."))
	}
	cl := apiClient()
	ctx := context.Background()
	flags := map[string]string{}
	parseFlags(args[1:], flags)
	switch args[0] {
	case "list", "ls":
		list, err := cl.ListDeployments(ctx, flags["device"], flags["name"])
		if err != nil {
			fatal(err)
		}
		for _, d := range list {
			health := "unhealthy"
			if d.HealthOK {
				health = "ok"
			}
			fmt.Printf("%s  %s  rev=%d  %s  health=%s  %s  %s\n", d.ID, d.Name, d.Revision, d.Status, health, d.Image, d.Listen)
		}
	case "create", "add":
		port := 0
		fmt.Sscanf(flags["port"], "%d", &port)
		if flags["device"] == "" || (flags["name"] == "" && flags["service"] == "") || flags["image"] == "" || port == 0 {
			fatal(fmt.Errorf("usage: knot deploy create --device ID --name NAME --image IMAGE --port N [--environment NAME]"))
		}
		name := flags["name"]
		if name == "" {
			name = flags["service"]
		}
		health := flags["health"]
		if health == "" {
			health = flags["health-path"]
		}
		dep, err := cl.CreateDeployment(ctx, client.CreateDeploymentRequest{
			DeviceID: flags["device"], Name: name, Image: flags["image"], Port: port,
			HealthPath: health, Hostname: flags["hostname"], EdgeDeviceID: flags["edge"],
			Environment: flags["environment"], EnvironmentID: flags["environment-id"], Project: flags["project"],
		})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("deployed %s  %s  rev=%d  %s\n", dep.Name, dep.Image, dep.Revision, dep.Status)
	case "show":
		id := flags["id"]
		if id == "" && len(args) >= 2 {
			id = args[1]
		}
		if id == "" {
			fatal(fmt.Errorf("usage: knot deploy show <id>"))
		}
		dep, err := cl.GetDeployment(ctx, id)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s  %s  rev=%d  %s  health_ok=%v  %s\n", dep.ID, dep.Name, dep.Revision, dep.Status, dep.HealthOK, dep.Image)
	case "stop":
		id := deployID(args, flags)
		dep, err := cl.StopDeployment(ctx, id)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("stopped %s\n", dep.Name)
	case "restart":
		id := deployID(args, flags)
		dep, err := cl.RestartDeployment(ctx, id)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("restarted %s  health_ok=%v\n", dep.Name, dep.HealthOK)
	case "rollback":
		id := deployID(args, flags)
		dep, err := cl.RollbackDeployment(ctx, id)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("rolled back %s  rev=%d  %s\n", dep.Name, dep.Revision, dep.Image)
	case "logs":
		id := deployID(args, flags)
		logs, err := cl.DeploymentLogs(ctx, id, 100)
		if err != nil {
			fatal(err)
		}
		for _, l := range logs {
			fmt.Printf("%s  %s\n", l.Stream, l.Message)
		}
	default:
		fatal(fmt.Errorf("unknown deploy subcommand: %s", args[0]))
	}
}

func deployID(args []string, flags map[string]string) string {
	id := flags["id"]
	if id == "" && len(args) >= 2 {
		id = args[1]
	}
	if id == "" {
		fatal(fmt.Errorf("deployment id required"))
	}
	return id
}

func parseFlags(args []string, out map[string]string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			continue
		}
		key := strings.TrimPrefix(a, "--")
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			out[key] = args[i+1]
			i++
		} else {
			out[key] = "true"
		}
	}
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func mustConfig() *config {
	c, err := loadConfig()
	if err != nil {
		fatal(fmt.Errorf("not logged in; run: knot login (%w)", err))
	}
	return c
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
