package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/cluster"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/database"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
)

func runHACommand(args []string) error {
	if len(args) == 0 {
		printHAHelp()
		return nil
	}

	switch args[0] {
	case "status":
		return runHAStatus(args[1:])
	case "assignments":
		return runHAAssignments(args[1:])
	case "reconcile":
		return runHAReconcile(args[1:])
	case "bootstrap":
		return runHABootstrap(args[1:])
	case "-h", "--help", "help":
		printHAHelp()
		return nil
	default:
		return fmt.Errorf("unsupported ha subcommand %q", args[0])
	}
}

func runHAAssignments(args []string) error {
	if len(args) == 0 {
		printHAAssignmentsHelp()
		return nil
	}

	switch args[0] {
	case "status":
		return runHAAssignmentsStatus(args[1:])
	case "drain":
		return runHAAssignmentsDrain(args[1:])
	case "release":
		return runHAAssignmentsRelease(args[1:])
	case "retry":
		return runHAAssignmentsRetry(args[1:])
	case "-h", "--help", "help":
		printHAAssignmentsHelp()
		return nil
	default:
		return fmt.Errorf("unsupported ha assignments subcommand %q", args[0])
	}
}

func runHAAssignmentsStatus(args []string) error {
	flags := newHAAssignmentFlags("assignments-status")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if help, _ := flags.GetBool("help"); help {
		fmt.Println("Usage:")
		fmt.Println("  warehouse ha assignments status -c config.yaml [--all] [--active-node-id ID] [--standby-node-id ID] [--state STATE] [--limit N]")
		return nil
	}

	cfg, err := loadHAConfigFromFlags(flags)
	if err != nil {
		return err
	}
	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer db.Close()

	filter, scope, err := resolveAssignmentStatusFilter(cfg, flags)
	if err != nil {
		return err
	}
	repo := repository.NewPostgresClusterReplicationAssignmentRepository(db.DB)
	assignments, err := repo.List(context.Background(), filter)
	if err != nil {
		return err
	}

	printPrettyJSONFromAny(buildAssignmentStatusResponse(cfg, filter, scope, assignments))
	return nil
}

func runHAAssignmentsDrain(args []string) error {
	return runHAAssignmentStateMutation(args, "drain")
}

func runHAAssignmentsRelease(args []string) error {
	return runHAAssignmentStateMutation(args, "release")
}

func runHAAssignmentsRetry(args []string) error {
	return runHAAssignmentStateMutation(args, "retry")
}

func runHAAssignmentStateMutation(args []string, action string) error {
	flags := newHAAssignmentMutationFlags("assignments-" + action)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if help, _ := flags.GetBool("help"); help {
		printHAAssignmentMutationUsage(action)
		return nil
	}

	cfg, err := loadHAConfigFromFlags(flags)
	if err != nil {
		return err
	}
	target, err := resolveAssignmentMutationTargetFromFlags(cfg, flags)
	if err != nil {
		return err
	}

	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer db.Close()

	assignmentRepo := repository.NewPostgresClusterReplicationAssignmentRepository(db.DB)
	assignment, err := assignmentRepo.GetByPair(context.Background(), target.ActiveNodeID, target.StandbyNodeID)
	if err != nil {
		return err
	}
	if assignment == nil {
		return fmt.Errorf("assignment %s -> %s not found", target.ActiveNodeID, target.StandbyNodeID)
	}

	standbyHealthy := false
	standbyLastHeartbeatAt := (*time.Time)(nil)
	if node, nodeErr := repository.NewPostgresClusterNodeRepository(db.DB).Get(context.Background(), target.StandbyNodeID); nodeErr == nil {
		healthy := node.Healthy(time.Now().UTC(), defaultHANodeStaleness)
		standbyHealthy = healthy
		standbyLastHeartbeatAt = &node.LastHeartbeatAt
	} else if nodeErr != nil && nodeErr != cluster.ErrNodeNotFound {
		return fmt.Errorf("load standby node %q: %w", target.StandbyNodeID, nodeErr)
	}

	force, _ := flags.GetBool("force")
	updated, changed, notes, err := planAssignmentStateMutation(action, assignment, standbyHealthy, force, time.Now().UTC())
	if err != nil {
		return err
	}
	if changed {
		if err := assignmentRepo.UpdateState(context.Background(), updated); err != nil {
			return err
		}
	}

	printPrettyJSONFromAny(buildAssignmentMutationResponse(action, changed, force, standbyHealthy, standbyLastHeartbeatAt, assignment, updated, notes))
	return nil
}

func runHAStatus(args []string) error {
	flags := newHAFlags("status")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if help, _ := flags.GetBool("help"); help {
		fmt.Println("Usage:")
		fmt.Println("  warehouse ha status -c config.yaml [--peer] [--base-url URL]")
		return nil
	}

	client, baseURL, err := buildHAClientFromFlags(flags)
	if err != nil {
		return err
	}
	body, err := client.doJSON(context.Background(), http.MethodGet, baseURL, "/api/v1/internal/replication/status", nil)
	if err != nil {
		return err
	}
	printPrettyJSON(body)
	return nil
}

func runHAReconcile(args []string) error {
	if len(args) == 0 {
		printHAReconcileHelp()
		return nil
	}

	switch args[0] {
	case "start":
		return runHAReconcileStart(args[1:])
	case "status":
		return runHAReconcileStatus(args[1:])
	case "-h", "--help", "help":
		printHAReconcileHelp()
		return nil
	default:
		return fmt.Errorf("unsupported ha reconcile subcommand %q", args[0])
	}
}

func runHAReconcileStart(args []string) error {
	flags := newHAFlags("reconcile-start")
	targetNodeID := flags.String("target-node-id", "", "Target standby node id override")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if help, _ := flags.GetBool("help"); help {
		fmt.Println("Usage:")
		fmt.Println("  warehouse ha reconcile start -c config.yaml [--peer] [--base-url URL] [--target-node-id ID]")
		return nil
	}
	client, baseURL, err := buildHAClientFromFlags(flags)
	if err != nil {
		return err
	}

	payload := map[string]string{}
	if strings.TrimSpace(*targetNodeID) != "" {
		payload["targetNodeId"] = strings.TrimSpace(*targetNodeID)
	}
	body, err := client.doJSON(context.Background(), http.MethodPost, baseURL, "/api/v1/internal/replication/reconcile/start", payload)
	if err != nil {
		return err
	}
	printPrettyJSON(body)
	return nil
}

func runHAReconcileStatus(args []string) error {
	flags := newHAFlags("reconcile-status")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if help, _ := flags.GetBool("help"); help {
		fmt.Println("Usage:")
		fmt.Println("  warehouse ha reconcile status -c config.yaml [--peer] [--base-url URL]")
		return nil
	}

	client, baseURL, err := buildHAClientFromFlags(flags)
	if err != nil {
		return err
	}
	body, err := client.doJSON(context.Background(), http.MethodGet, baseURL, "/api/v1/internal/replication/status", nil)
	if err != nil {
		return err
	}

	var statusResp map[string]any
	if err := json.Unmarshal(body, &statusResp); err != nil {
		return fmt.Errorf("parse status response: %w", err)
	}
	reconcile, ok := statusResp["reconcile"]
	if !ok {
		fmt.Println("{}")
		return nil
	}
	reconcileBody, err := json.Marshal(reconcile)
	if err != nil {
		return fmt.Errorf("marshal reconcile status: %w", err)
	}
	printPrettyJSON(reconcileBody)
	return nil
}

func runHABootstrap(args []string) error {
	if len(args) == 0 {
		printHABootstrapHelp()
		return nil
	}

	switch args[0] {
	case "mark":
		return runHABootstrapMark(args[1:])
	case "-h", "--help", "help":
		printHABootstrapHelp()
		return nil
	default:
		return fmt.Errorf("unsupported ha bootstrap subcommand %q", args[0])
	}
}

func runHABootstrapMark(args []string) error {
	flags := newHAFlags("bootstrap-mark")
	outboxID := flags.Int64("outbox-id", -1, "Explicit bootstrap outbox id")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if help, _ := flags.GetBool("help"); help {
		fmt.Println("Usage:")
		fmt.Println("  warehouse ha bootstrap mark -c config.yaml [--peer] [--base-url URL] [--outbox-id N]")
		return nil
	}
	client, baseURL, err := buildHAClientFromFlags(flags)
	if err != nil {
		return err
	}
	if client.assignmentGeneration == nil || *client.assignmentGeneration <= 0 {
		peer, resolveErr := resolvePeerFromControlPlane(client.cfg)
		if resolveErr != nil {
			return fmt.Errorf("bootstrap mark requires a resolved peer assignment generation: %w", resolveErr)
		}
		if peer != nil {
			client.assignmentGeneration = peer.AssignmentGeneration
		}
	}
	if client.assignmentGeneration == nil || *client.assignmentGeneration <= 0 {
		return fmt.Errorf("bootstrap mark requires a resolved peer assignment generation; use --peer with an assigned standby")
	}

	payload := map[string]int64{}
	if *outboxID >= 0 {
		payload["outboxId"] = *outboxID
	}
	var body []byte
	if len(payload) == 0 {
		body, err = client.doJSON(context.Background(), http.MethodPost, baseURL, "/api/v1/internal/replication/bootstrap/mark", map[string]any{})
	} else {
		body, err = client.doJSON(context.Background(), http.MethodPost, baseURL, "/api/v1/internal/replication/bootstrap/mark", payload)
	}
	if err != nil {
		return err
	}
	printPrettyJSON(body)
	return nil
}

type haClient struct {
	cfg                  *config.Config
	httpClient           *http.Client
	assignmentGeneration *int64
}

func newHAFlags(name string) *pflag.FlagSet {
	flags := pflag.NewFlagSet(name, pflag.ContinueOnError)
	flags.StringP("config", "c", "", "Config file path")
	flags.String("base-url", "", "Internal base URL override, such as http://127.0.0.1:6065")
	flags.Bool("peer", false, "Use internal.replication.peer_base_url from config")
	flags.Duration("timeout", 0, "HTTP timeout override")
	flags.BoolP("help", "h", false, "Show help")
	return flags
}

func buildHAClientFromFlags(flags *pflag.FlagSet) (*haClient, string, error) {
	cfg, err := loadHAConfigFromFlags(flags)
	if err != nil {
		return nil, "", err
	}

	baseURL, assignmentGeneration, err := resolveHATarget(cfg, flags)
	if err != nil {
		return nil, "", err
	}

	timeout, _ := flags.GetDuration("timeout")
	if timeout <= 0 {
		timeout = cfg.Internal.Replication.RequestTimeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
	}

	return &haClient{
		cfg:                  cfg,
		assignmentGeneration: assignmentGeneration,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, baseURL, nil
}

func loadHAConfigFromFlags(flags *pflag.FlagSet) (*config.Config, error) {
	configFile, _ := flags.GetString("config")
	if strings.TrimSpace(configFile) == "" {
		return nil, fmt.Errorf("config file is required, use -c config.yaml")
	}
	return loadConfig(configFile, nil)
}

func resolveHABaseURL(cfg *config.Config, flags *pflag.FlagSet) (string, error) {
	baseURL, _, err := resolveHATarget(cfg, flags)
	return baseURL, err
}

func resolveHATarget(cfg *config.Config, flags *pflag.FlagSet) (string, *int64, error) {
	baseURL, _ := flags.GetString("base-url")
	if strings.TrimSpace(baseURL) != "" {
		normalized, err := normalizeURL(baseURL)
		return normalized, nil, err
	}

	usePeer, _ := flags.GetBool("peer")
	if usePeer {
		peer, err := resolveHAPeer(cfg)
		if err != nil {
			return "", nil, err
		}
		if peer == nil || strings.TrimSpace(peer.BaseURL) == "" {
			return "", nil, fmt.Errorf("failed to resolve peer base url from config or cluster registry")
		}
		normalized, err := normalizeURL(peer.BaseURL)
		return normalized, peer.AssignmentGeneration, err
	}

	scheme := "http"
	if cfg.Server.TLS {
		scheme = "https"
	}
	host := strings.TrimSpace(cfg.Server.Address)
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	normalized, err := normalizeURL(fmt.Sprintf("%s://%s:%d", scheme, host, cfg.Server.Port))
	return normalized, nil, err
}

func resolveHAPeer(cfg *config.Config) (*service.ResolvedReplicationPeer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	nodeID := strings.TrimSpace(cfg.Internal.Replication.PeerNodeID)
	baseURL := strings.TrimSpace(cfg.Internal.Replication.PeerBaseURL)
	if baseURL != "" {
		return &service.ResolvedReplicationPeer{
			NodeID:  nodeID,
			BaseURL: baseURL,
			Source:  "config",
		}, nil
	}
	return resolvePeerFromControlPlane(cfg)
}

func resolvePeerFromControlPlane(cfg *config.Config) (*service.ResolvedReplicationPeer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("connect database for peer resolution: %w", err)
	}
	defer db.Close()

	resolver := service.NewReplicationPeerResolver(
		cfg,
		repository.NewPostgresClusterNodeRepository(db.DB),
		repository.NewPostgresClusterReplicationAssignmentRepository(db.DB),
	)
	if resolver == nil {
		return nil, nil
	}
	peer, err := resolver.ResolveTarget(context.Background())
	if err != nil {
		return nil, fmt.Errorf("resolve peer from control plane: %w", err)
	}
	return peer, nil
}

func (c *haClient) doJSON(ctx context.Context, method, baseURL, path string, payload any) ([]byte, error) {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal request payload: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.signRequest(req, body)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(respBody))
		if message == "" {
			message = resp.Status
		}
		return nil, fmt.Errorf("request failed: %s", message)
	}
	return respBody, nil
}

func (c *haClient) signRequest(req *http.Request, body []byte) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	payloadHash := "UNSIGNED-PAYLOAD"
	if len(body) > 0 {
		payloadHash = payloadSHA256(body)
	}
	req.Header.Set(middleware.InternalNodeIDHeader, strings.TrimSpace(c.cfg.Node.ID))
	req.Header.Set(middleware.InternalTimestampHeader, timestamp)
	req.Header.Set(middleware.InternalContentSHA256Header, payloadHash)
	if c.assignmentGeneration != nil && *c.assignmentGeneration > 0 {
		req.Header.Set(middleware.InternalAssignmentGenerationHeader, fmt.Sprintf("%d", *c.assignmentGeneration))
	}
	req.Header.Set(middleware.InternalSignatureHeader, middleware.SignInternalRequest(
		req.Method,
		req.URL.Path,
		strings.TrimSpace(c.cfg.Node.ID),
		timestamp,
		payloadHash,
		strings.TrimSpace(c.cfg.Internal.Replication.SharedSecret),
	))
}

func payloadSHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func printPrettyJSON(body []byte) {
	var out bytes.Buffer
	if err := json.Indent(&out, body, "", "  "); err != nil {
		fmt.Println(string(body))
		return
	}
	fmt.Println(out.String())
}

func printPrettyJSONFromAny(data any) {
	body, err := json.Marshal(data)
	if err != nil {
		fmt.Println(fmt.Sprintf("{\"error\":%q}", err.Error()))
		return
	}
	printPrettyJSON(body)
}

func printHAHelp() {
	fmt.Println("Usage:")
	fmt.Println("  warehouse ha status -c config.yaml [--peer] [--base-url URL]")
	fmt.Println("  warehouse ha assignments status -c config.yaml [--all] [--active-node-id ID] [--standby-node-id ID] [--state STATE] [--limit N]")
	fmt.Println("  warehouse ha assignments drain -c config.yaml --standby-node-id ID [--active-node-id ID]")
	fmt.Println("  warehouse ha assignments release -c config.yaml --standby-node-id ID [--active-node-id ID] [--force]")
	fmt.Println("  warehouse ha assignments retry -c config.yaml --standby-node-id ID [--active-node-id ID]")
	fmt.Println("  warehouse ha reconcile start -c config.yaml [--peer] [--base-url URL] [--target-node-id ID]")
	fmt.Println("  warehouse ha reconcile status -c config.yaml [--peer] [--base-url URL]")
	fmt.Println("  warehouse ha bootstrap mark -c config.yaml [--peer] [--base-url URL] [--outbox-id N]")
}

func printHAAssignmentsHelp() {
	fmt.Println("Usage:")
	fmt.Println("  warehouse ha assignments status -c config.yaml [--all] [--active-node-id ID] [--standby-node-id ID] [--state STATE] [--limit N]")
	fmt.Println("  warehouse ha assignments drain -c config.yaml --standby-node-id ID [--active-node-id ID]")
	fmt.Println("  warehouse ha assignments release -c config.yaml --standby-node-id ID [--active-node-id ID] [--force]")
	fmt.Println("  warehouse ha assignments retry -c config.yaml --standby-node-id ID [--active-node-id ID]")
}

func printHAReconcileHelp() {
	fmt.Println("Usage:")
	fmt.Println("  warehouse ha reconcile start -c config.yaml [--peer] [--base-url URL] [--target-node-id ID]")
	fmt.Println("  warehouse ha reconcile status -c config.yaml [--peer] [--base-url URL]")
}

func printHABootstrapHelp() {
	fmt.Println("Usage:")
	fmt.Println("  warehouse ha bootstrap mark -c config.yaml [--peer] [--base-url URL] [--outbox-id N]")
}

func newHAAssignmentFlags(name string) *pflag.FlagSet {
	flags := pflag.NewFlagSet(name, pflag.ContinueOnError)
	flags.StringP("config", "c", "", "Config file path")
	flags.Bool("all", false, "List assignments for all nodes")
	flags.String("active-node-id", "", "Filter by active node id")
	flags.String("standby-node-id", "", "Filter by standby node id")
	flags.String("state", "", "Filter by assignment state")
	flags.Int("limit", 20, "Maximum number of assignments to return")
	flags.BoolP("help", "h", false, "Show help")
	return flags
}

type haAssignmentStatusResponse struct {
	Node        haAssignmentNodeStatus     `json:"node"`
	Scope       string                     `json:"scope"`
	Filters     haAssignmentStatusFilters  `json:"filters"`
	Count       int                        `json:"count"`
	Assignments []haAssignmentStatusRecord `json:"assignments"`
	Notes       []string                   `json:"notes,omitempty"`
}

type haAssignmentMutationTarget struct {
	ActiveNodeID  string
	StandbyNodeID string
}

type haAssignmentMutationResponse struct {
	Action               string                   `json:"action"`
	Changed              bool                     `json:"changed"`
	Forced               bool                     `json:"forced"`
	StandbyHealthy       bool                     `json:"standbyHealthy"`
	StandbyLastHeartbeat *time.Time               `json:"standbyLastHeartbeatAt,omitempty"`
	Before               haAssignmentStatusRecord `json:"before"`
	After                haAssignmentStatusRecord `json:"after"`
	Notes                []string                 `json:"notes,omitempty"`
}

type haAssignmentNodeStatus struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

type haAssignmentStatusFilters struct {
	ActiveNodeID  string `json:"activeNodeId,omitempty"`
	StandbyNodeID string `json:"standbyNodeId,omitempty"`
	State         string `json:"state,omitempty"`
	Limit         int    `json:"limit"`
}

type haAssignmentStatusRecord struct {
	ID                 int64      `json:"id"`
	ActiveNodeID       string     `json:"activeNodeId"`
	StandbyNodeID      string     `json:"standbyNodeId"`
	State              string     `json:"state"`
	Generation         int64      `json:"generation"`
	LeaseExpiresAt     *time.Time `json:"leaseExpiresAt,omitempty"`
	LeaseExpired       bool       `json:"leaseExpired"`
	LastReconcileJobID *int64     `json:"lastReconcileJobId,omitempty"`
	LastError          *string    `json:"lastError,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

const defaultHANodeStaleness = 15 * time.Second

func resolveAssignmentStatusFilter(cfg *config.Config, flags *pflag.FlagSet) (repository.ClusterReplicationAssignmentFilter, string, error) {
	filter := repository.ClusterReplicationAssignmentFilter{}
	scope := "filtered"

	all, _ := flags.GetBool("all")
	limit, _ := flags.GetInt("limit")
	filter.Limit = limit
	activeNodeID, _ := flags.GetString("active-node-id")
	standbyNodeID, _ := flags.GetString("standby-node-id")
	state, _ := flags.GetString("state")
	filter.ActiveNodeID = strings.TrimSpace(activeNodeID)
	filter.StandbyNodeID = strings.TrimSpace(standbyNodeID)
	filter.State = strings.TrimSpace(state)

	if all {
		scope = "all"
		return filter, scope, nil
	}
	if filter.ActiveNodeID != "" || filter.StandbyNodeID != "" || filter.State != "" {
		return filter, scope, nil
	}

	if cfg == nil {
		return filter, scope, nil
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Node.Role)) {
	case "active":
		if strings.TrimSpace(cfg.Node.ID) != "" {
			filter.ActiveNodeID = strings.TrimSpace(cfg.Node.ID)
			scope = "current_active"
		}
	case "standby":
		if strings.TrimSpace(cfg.Node.ID) != "" {
			filter.StandbyNodeID = strings.TrimSpace(cfg.Node.ID)
			scope = "current_standby"
		}
	default:
		scope = "all"
	}
	return filter, scope, nil
}

func buildAssignmentStatusResponse(
	cfg *config.Config,
	filter repository.ClusterReplicationAssignmentFilter,
	scope string,
	assignments []*cluster.ReplicationAssignment,
) haAssignmentStatusResponse {
	resp := haAssignmentStatusResponse{
		Scope: scope,
		Filters: haAssignmentStatusFilters{
			ActiveNodeID:  strings.TrimSpace(filter.ActiveNodeID),
			StandbyNodeID: strings.TrimSpace(filter.StandbyNodeID),
			State:         strings.TrimSpace(filter.State),
			Limit:         filter.Limit,
		},
		Count:       len(assignments),
		Assignments: make([]haAssignmentStatusRecord, 0, len(assignments)),
	}
	if cfg != nil {
		resp.Node = haAssignmentNodeStatus{
			ID:   strings.TrimSpace(cfg.Node.ID),
			Role: strings.TrimSpace(cfg.Node.Role),
		}
	}

	now := time.Now().UTC()
	for _, assignment := range assignments {
		if assignment == nil {
			continue
		}
		resp.Assignments = append(resp.Assignments, haAssignmentStatusRecord{
			ID:                 assignment.ID,
			ActiveNodeID:       assignment.ActiveNodeID,
			StandbyNodeID:      assignment.StandbyNodeID,
			State:              assignment.State,
			Generation:         assignment.Generation,
			LeaseExpiresAt:     assignment.LeaseExpiresAt,
			LeaseExpired:       assignment.LeaseExpired(now),
			LastReconcileJobID: assignment.LastReconcileJobID,
			LastError:          assignment.LastError,
			CreatedAt:          assignment.CreatedAt,
			UpdatedAt:          assignment.UpdatedAt,
		})
	}
	resp.Notes = []string{
		"cluster_replication_assignments is maintained by the active-side allocator/renewer",
		"current assignment state is control-plane observable; replication traffic still follows the existing single-target pipeline",
	}
	return resp
}

func newHAAssignmentMutationFlags(name string) *pflag.FlagSet {
	flags := pflag.NewFlagSet(name, pflag.ContinueOnError)
	flags.StringP("config", "c", "", "Config file path")
	flags.String("active-node-id", "", "Active node id")
	flags.String("standby-node-id", "", "Standby node id")
	flags.Bool("force", false, "Bypass safety validation when supported")
	flags.BoolP("help", "h", false, "Show help")
	return flags
}

func printHAAssignmentMutationUsage(action string) {
	fmt.Println("Usage:")
	switch action {
	case "release":
		fmt.Println("  warehouse ha assignments release -c config.yaml --standby-node-id ID [--active-node-id ID] [--force]")
	case "retry":
		fmt.Println("  warehouse ha assignments retry -c config.yaml --standby-node-id ID [--active-node-id ID]")
	default:
		fmt.Printf("  warehouse ha assignments %s -c config.yaml --standby-node-id ID [--active-node-id ID]\n", action)
	}
}

func resolveAssignmentMutationTargetFromFlags(cfg *config.Config, flags *pflag.FlagSet) (haAssignmentMutationTarget, error) {
	activeNodeID, _ := flags.GetString("active-node-id")
	standbyNodeID, _ := flags.GetString("standby-node-id")
	return resolveAssignmentMutationTarget(cfg, activeNodeID, standbyNodeID)
}

func resolveAssignmentMutationTarget(cfg *config.Config, activeNodeID, standbyNodeID string) (haAssignmentMutationTarget, error) {
	target := haAssignmentMutationTarget{
		ActiveNodeID:  strings.TrimSpace(activeNodeID),
		StandbyNodeID: strings.TrimSpace(standbyNodeID),
	}
	if cfg != nil {
		switch strings.ToLower(strings.TrimSpace(cfg.Node.Role)) {
		case "active":
			if target.ActiveNodeID == "" {
				target.ActiveNodeID = strings.TrimSpace(cfg.Node.ID)
			}
		case "standby":
			if target.StandbyNodeID == "" {
				target.StandbyNodeID = strings.TrimSpace(cfg.Node.ID)
			}
		}
	}
	if target.ActiveNodeID == "" {
		return target, fmt.Errorf("active node id is required; use --active-node-id or run from an active config")
	}
	if target.StandbyNodeID == "" {
		return target, fmt.Errorf("standby node id is required; use --standby-node-id or run from a standby config")
	}
	return target, nil
}

func planAssignmentStateMutation(
	action string,
	assignment *cluster.ReplicationAssignment,
	standbyHealthy bool,
	force bool,
	now time.Time,
) (*cluster.ReplicationAssignment, bool, []string, error) {
	if assignment == nil {
		return nil, false, nil, fmt.Errorf("assignment is required")
	}
	assignment.Normalize()
	updated := *assignment
	notes := make([]string, 0, 3)

	switch strings.ToLower(strings.TrimSpace(action)) {
	case "drain":
		if assignment.State == cluster.AssignmentStateDraining {
			notes = append(notes, "assignment is already in draining state")
			return &updated, false, notes, nil
		}
		if !cluster.CanTransitionAssignmentState(assignment.State, cluster.AssignmentStateDraining) {
			return nil, false, nil, fmt.Errorf("assignment %s -> %s in state %q cannot transition to draining", assignment.ActiveNodeID, assignment.StandbyNodeID, assignment.State)
		}
		updated.State = cluster.AssignmentStateDraining
		notes = append(notes, "draining keeps the assignment effective; standby will still accept the assigned active")
		return &updated, true, notes, nil
	case "release":
		if assignment.State == cluster.AssignmentStateReleased {
			notes = append(notes, "assignment is already released")
			return &updated, false, notes, nil
		}
		if !cluster.CanTransitionAssignmentState(assignment.State, cluster.AssignmentStateReleased) {
			if !force {
				return nil, false, nil, fmt.Errorf("assignment %s -> %s in state %q cannot transition to released without --force", assignment.ActiveNodeID, assignment.StandbyNodeID, assignment.State)
			}
			notes = append(notes, "release was forced from a non-standard state transition")
		}
		if standbyHealthy && !force {
			return nil, false, nil, fmt.Errorf("standby %q is still healthy; stop or isolate it first, or retry with --force", assignment.StandbyNodeID)
		}
		if standbyHealthy && force {
			notes = append(notes, "release was forced while standby heartbeat is still healthy; allocator may re-assign it if the active keeps running")
		}
		expiredAt := now.UTC()
		updated.State = cluster.AssignmentStateReleased
		updated.LeaseExpiresAt = &expiredAt
		return &updated, true, notes, nil
	case "retry":
		if assignment.State == cluster.AssignmentStatePending {
			notes = append(notes, "assignment is already pending")
			return &updated, false, notes, nil
		}
		if !cluster.CanTransitionAssignmentState(assignment.State, cluster.AssignmentStatePending) {
			return nil, false, nil, fmt.Errorf("assignment %s -> %s in state %q cannot transition to pending", assignment.ActiveNodeID, assignment.StandbyNodeID, assignment.State)
		}
		updated.State = cluster.AssignmentStatePending
		updated.LastError = nil
		notes = append(notes, "retry moves the assignment back to pending so active can re-run reconcile and resume generation-bound replication")
		return &updated, true, notes, nil
	default:
		return nil, false, nil, fmt.Errorf("unsupported assignment action %q", action)
	}
}

func buildAssignmentMutationResponse(
	action string,
	changed bool,
	force bool,
	standbyHealthy bool,
	standbyLastHeartbeatAt *time.Time,
	before *cluster.ReplicationAssignment,
	after *cluster.ReplicationAssignment,
	notes []string,
) haAssignmentMutationResponse {
	now := time.Now().UTC()
	resp := haAssignmentMutationResponse{
		Action:               action,
		Changed:              changed,
		Forced:               force,
		StandbyHealthy:       standbyHealthy,
		StandbyLastHeartbeat: standbyLastHeartbeatAt,
		Notes:                append([]string(nil), notes...),
	}
	if before != nil {
		resp.Before = haAssignmentStatusRecord{
			ID:                 before.ID,
			ActiveNodeID:       before.ActiveNodeID,
			StandbyNodeID:      before.StandbyNodeID,
			State:              before.State,
			Generation:         before.Generation,
			LeaseExpiresAt:     before.LeaseExpiresAt,
			LeaseExpired:       before.LeaseExpired(now),
			LastReconcileJobID: before.LastReconcileJobID,
			LastError:          before.LastError,
			CreatedAt:          before.CreatedAt,
			UpdatedAt:          before.UpdatedAt,
		}
	}
	if after != nil {
		resp.After = haAssignmentStatusRecord{
			ID:                 after.ID,
			ActiveNodeID:       after.ActiveNodeID,
			StandbyNodeID:      after.StandbyNodeID,
			State:              after.State,
			Generation:         after.Generation,
			LeaseExpiresAt:     after.LeaseExpiresAt,
			LeaseExpired:       after.LeaseExpired(now),
			LastReconcileJobID: after.LastReconcileJobID,
			LastError:          after.LastError,
			CreatedAt:          after.CreatedAt,
			UpdatedAt:          after.UpdatedAt,
		}
	}
	return resp
}

func normalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("base URL must include scheme and host")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}
