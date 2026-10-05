// Package natsbus provides a NATS-backed implementation of the event
// Publisher and Subscriber interfaces. Events are JSON-serialized and sent
// to a subject derived from the event's schema and table. The default subject
// pattern is "sqlgen.events.{schema}.{table}"; the prefix is configurable via
// WithPrefix. Consumer groups from event.SubscribeOptions.Group map to NATS
// queue groups.
//
// By default this transport is at-most-once and does not provide persistence.
// Passing WithJetStream switches it to durable JetStream mode, publishing to a
// persisted stream with broker-side deduplication (section 28.7.1). Durable,
// at-least-once consumers with explicit acknowledgement are created via
// SubscribeWith; the plain Subscribe stays a core real-time tap that satisfies
// event.Subscriber.
package natsbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/teandresmith/sqlgen/event"
)

// DefaultPrefix is the default subject prefix for NATS events.
const DefaultPrefix = "sqlgen.events"

// Connection resilience defaults applied by Connect. They are set explicitly so
// Connect's contract does not depend on nats.go's own defaults.
const (
	// defaultReconnectBufSize sizes the buffer that holds messages published
	// while the connection is reconnecting, so a brief broker outage does not
	// drop publishes. It matches nats.go's own default (8 MiB), pinned here.
	defaultReconnectBufSize = 8 * 1024 * 1024
	// defaultDrainTimeout bounds how long Close's Drain waits for in-flight
	// messages to flush before the connection is torn down.
	defaultDrainTimeout = 30 * time.Second
)

// Wire header names carried as NATS headers, out of band from the JSON
// envelope body (section 28.7.1). They are single-sourced here for reuse
// across publish, trace-context propagation, and JetStream dedup. Older
// consumers ignore unknown headers, so header additions are backward
// compatible.
const (
	// headerEnvelopeVersion labels the wire-scheme version so consumers can
	// detect a scheme change. Its value is envelopeVersion; any change to the
	// subject scheme or body encoding bumps that constant.
	headerEnvelopeVersion = "Sqlgen-Envelope-Version"
	// headerMsgID is the JetStream broker-side dedup key, set to Event.ID in
	// JetStream mode (used by the JetStream publish path).
	headerMsgID = "Nats-Msg-Id"
	// headerTraceparent carries W3C trace context lifted from
	// Event.Metadata["traceparent"] (used by trace-context propagation).
	headerTraceparent = "traceparent"
)

// Envelope-version values stamped on every published message via
// headerEnvelopeVersion. The default scheme — "{prefix}.{schema}.{table}"
// subject with a snake_case JSON body — is version 1. WithActionSubjects
// appends a "{action}" subject token — a different subject scheme — so it
// stamps version 2. Bump these on any further subject-scheme or body-encoding
// change.
const (
	envelopeVersion               = "1"
	envelopeVersionActionSubjects = "2"
)

// Option configures a Bus at construction time.
type Option func(*Bus)

// Propagator is the natsbus-local adapter a consumer supplies to carry W3C
// trace context across the async publish/subscribe hop (section 28.7.1). It
// keeps OpenTelemetry — or any tracing library — out of the natsbus module's
// go.mod: the consumer implements it by wrapping their own propagator over
// nats.Header.
//
// On the subscribe side, natsbus calls Extract (when WithContextPropagation is
// set) to rebuild a span-continuing ctx for the Handler. On the publish side
// natsbus does not call Inject: under the default async-commit path the publish
// runs on context.Background() (section 18.5, §1.2), so there is no live span to
// inject from; instead the producer's MetadataFunc (section 28.8) captures
// "traceparent" into Event.Metadata at hook entry and Publish lifts that value
// onto the traceparent header directly. Inject completes the adapter contract so
// a consumer can reuse the same value on their own publish paths.
type Propagator interface {
	// Inject writes the trace context carried by ctx into h.
	Inject(ctx context.Context, h nats.Header)
	// Extract returns a ctx derived from parent that continues the trace
	// context carried by h. It must return a non-nil ctx (parent unchanged
	// when h carries no trace context).
	Extract(parent context.Context, h nats.Header) context.Context
}

// WithPrefix overrides the subject prefix. The resulting subject for an
// event is "{prefix}.{schema}.{table}" (or "{prefix}.{table}" when Schema
// is empty).
func WithPrefix(prefix string) Option {
	return func(b *Bus) {
		b.prefix = prefix
	}
}

// WithActionSubjects publishes to the action-qualified subject
// "{prefix}.{schema}.{table}.{action}" instead of the default
// "{prefix}.{schema}.{table}", so a consumer can let the broker filter by
// action (e.g. subscribe only to "...tasks.create") rather than filtering every
// decoded event in the handler (section 28.7.1). It changes the subject scheme,
// so it is opt-in and stamps a bumped Sqlgen-Envelope-Version (2). Subscriptions
// created on the same bus derive the matching action-qualified subject, so a
// single Table + single Action narrows delivery broker-side; the per-message
// filter still applies for multi-table or multi-action subscriptions. The JSON
// body is unchanged.
func WithActionSubjects() Option {
	return func(b *Bus) {
		b.actionSubjects = true
	}
}

// WithSubscribeErrorHandler registers a handler invoked when a subscription
// Handler returns a non-nil error. Core NATS is at-most-once and cannot
// redeliver, so a handler error is otherwise discarded; registering this makes
// the failure observable (section 28.5). The default — no handler — discards
// the error without panicking and without redelivering, preserving the
// zero-config behavior. On JetStream (section 28.7.1) broker redelivery
// supersedes this.
func WithSubscribeErrorHandler(fn func(ctx context.Context, e event.Event, err error)) Option {
	return func(b *Bus) {
		b.subErrHandler = fn
	}
}

// WithRetry bounds in-process retry of a subscription Handler on core
// (at-most-once) mode: the handler is invoked up to n times, sleeping backoff
// between attempts, until it returns nil. If every attempt fails the final
// error is routed to the WithSubscribeErrorHandler handler when one is set.
// n < 1 is treated as a single attempt (no retry) and a non-positive backoff
// sleeps not at all. Retry is a core-mode concept — JetStream redelivery
// (section 28.7.1) supersedes it.
func WithRetry(n int, backoff time.Duration) Option {
	return func(b *Bus) {
		b.retryAttempts = n
		b.retryBackoff = backoff
	}
}

// WithContextPropagation registers a Propagator so that a subscription Handler
// receives a ctx continuing the producing request's span instead of a
// context-less context.Background() (section 28.7.1). When set, the subscribe
// path reads the traceparent header (lifted from Event.Metadata on publish) and
// hands the Handler adapter.Extract's ctx. Without it, traceparent still travels
// on the wire header but the Handler ctx is the default — no crash, no tracing
// dependency. This is a subscribe-side option only; publish always carries the
// header when Event.Metadata["traceparent"] is present.
func WithContextPropagation(p Propagator) Option {
	return func(b *Bus) {
		b.propagator = p
	}
}

// StreamConfig describes the JetStream stream that WithJetStream provisions at
// construction (section 28.7.1). The stream captures every subject under the
// bus prefix ("{prefix}.>"), so all published events are persisted to it.
type StreamConfig struct {
	// Name is the JetStream stream name (e.g. "SQLGEN_EVENTS").
	Name string
	// MaxAge is how long messages are retained before the broker ages them
	// out. Zero means unlimited (subject to the server's storage limits).
	MaxAge time.Duration
	// DedupWindow is the broker-side deduplication window keyed on the
	// Nats-Msg-Id header (set to Event.ID on publish): two publishes of the
	// same Event.ID within this window collapse to one stored message. Zero
	// uses the server default window.
	DedupWindow time.Duration
}

// WithJetStream switches the bus into durable JetStream mode (section 28.7.1).
// It is opt-in: without it, New(conn) is byte-for-byte the core at-most-once
// transport and provisions nothing. When passed, New obtains a JetStream context
// and ensures the described stream exists (create-or-update with the given MaxAge
// and DedupWindow); a provisioning failure is captured and surfaced from the
// first Publish/Subscribe rather than silently swallowed. In JetStream mode
// Publish targets the persisted stream and stamps the Nats-Msg-Id header with
// Event.ID so the broker's dedup window collapses duplicate publishes with no
// consumer-side bookkeeping.
func WithJetStream(cfg StreamConfig) Option {
	return func(b *Bus) {
		b.streamCfg = &cfg
	}
}

// SubscribeOption configures a single durable JetStream subscription created via
// (*Bus).SubscribeWith (section 28.7.1). These knobs are natsbus-local and
// deliberately do NOT live on the shared event.SubscribeOptions, which stays the
// stdlib-only, transport-agnostic filter type. In core mode (no WithJetStream)
// they have no effect — SubscribeWith degrades to the same at-most-once core tap
// as Subscribe.
type SubscribeOption func(*subscribeConfig)

// subscribeConfig accumulates the natsbus-local SubscribeOption values for one
// SubscribeWith call.
type subscribeConfig struct {
	durable    string
	backoff    time.Duration
	maxDeliver int
	deadLetter string

	// Delivery-start policy for replay/backfill (section 28.7.1). The zero
	// value is DeliverAllPolicy, so an un-set config replays the full retained
	// stream history — byte-identical to the JetStream consumer default.
	// optStartTime and optStartSeq are only consulted for the by-time and
	// by-sequence policies.
	deliverPolicy jetstream.DeliverPolicy
	optStartTime  *time.Time
	optStartSeq   uint64
}

// WithDurable binds the subscription to a named durable JetStream consumer so
// its delivery cursor and redelivery state survive process restarts and are
// shared by every subscriber using the same name (a load-balanced group).
// Without it, SubscribeWith creates an unnamed consumer that still acks/naks
// (at-least-once for the life of the process) but is not durable across
// restarts. It has no effect in core mode.
func WithDurable(name string) SubscribeOption {
	return func(c *subscribeConfig) {
		c.durable = name
	}
}

// WithBackoff sets the redelivery delay applied when a Handler returns a non-nil
// error on a JetStream subscription: the message is negatively acknowledged with
// this delay before the broker redelivers it (section 28.5 error-as-Nak). Zero
// naks for immediate redelivery, letting the consumer's AckWait govern timing.
// It has no effect in core mode (core cannot redeliver).
func WithBackoff(d time.Duration) SubscribeOption {
	return func(c *subscribeConfig) {
		c.backoff = d
	}
}

// WithMaxDeliver caps the number of times a JetStream message is delivered before
// the broker stops redelivering it (section 28.7.1). It bounds the error-as-Nak
// redelivery loop: after n failed deliveries the message is no longer retried.
// Paired with WithDeadLetter, the final failed delivery routes the message to the
// dead-letter subject; without WithDeadLetter, the broker's default terminal
// behavior applies (the message is left un-acked, not redelivered, no crash).
// n <= 0 leaves delivery unbounded (the JetStream default). It has no effect in
// core mode.
func WithMaxDeliver(n int) SubscribeOption {
	return func(c *subscribeConfig) {
		c.maxDeliver = n
	}
}

// WithDeadLetter names a subject that receives a message whose Handler has failed
// its maximum number of deliveries (section 28.7.1). On the final permitted
// delivery (see WithMaxDeliver) the original envelope is republished to this
// subject with its headers intact — Event.ID (Nats-Msg-Id), traceparent, and the
// envelope version all survive for downstream inspection — and the stream message
// is terminated so the broker stops redelivering it. It requires WithMaxDeliver to
// define "maximum deliveries"; without a positive max-deliver bound no message is
// ever routed. It has no effect in core mode.
func WithDeadLetter(subject string) SubscribeOption {
	return func(c *subscribeConfig) {
		c.deadLetter = subject
	}
}

// WithDeliverAll makes a fresh durable JetStream consumer replay the entire
// retained stream history — every message still within the stream's MaxAge — from
// the beginning, rather than only messages published after the consumer is created
// (section 28.7.1, replay/backfill). It is how a newly added projection rebuilds
// itself from the event log. This is also the consumer default, so calling it is
// only needed to be explicit or to override a start-point set earlier in the same
// option list. It has no effect in core mode.
func WithDeliverAll() SubscribeOption {
	return func(c *subscribeConfig) {
		c.deliverPolicy = jetstream.DeliverAllPolicy
		c.optStartTime = nil
		c.optStartSeq = 0
	}
}

// WithDeliverFromTime replays a fresh durable JetStream consumer from the first
// message stored at or after t, skipping earlier history (section 28.7.1). Use it
// to backfill a projection from a known point in time rather than the whole log.
// It has no effect in core mode.
func WithDeliverFromTime(t time.Time) SubscribeOption {
	return func(c *subscribeConfig) {
		c.deliverPolicy = jetstream.DeliverByStartTimePolicy
		c.optStartTime = &t
		c.optStartSeq = 0
	}
}

// WithDeliverFromSequence replays a fresh durable JetStream consumer from the given
// stream sequence number, skipping earlier messages (section 28.7.1). Use it to
// resume a backfill from a recorded offset. It has no effect in core mode.
func WithDeliverFromSequence(seq uint64) SubscribeOption {
	return func(c *subscribeConfig) {
		c.deliverPolicy = jetstream.DeliverByStartSequencePolicy
		c.optStartSeq = seq
		c.optStartTime = nil
	}
}

// Bus is a NATS event.Publisher and event.Subscriber. It is safe for
// concurrent use by multiple goroutines.
type Bus struct {
	conn   *nats.Conn
	prefix string

	// actionSubjects, when set by WithActionSubjects, appends a "{action}"
	// segment to published subjects and derives action-qualified subscribe
	// subjects. Set once at construction, read-only after New returns.
	actionSubjects bool

	// Subscriber error-handling config (set once at construction, read-only
	// after New returns, so callback goroutines observe it without locking).
	subErrHandler func(ctx context.Context, e event.Event, err error)
	retryAttempts int
	retryBackoff  time.Duration
	propagator    Propagator

	// JetStream config (set once at construction, read-only after New returns).
	// streamCfg is non-nil when WithJetStream was passed; js is the JetStream
	// context obtained at construction; jsErr captures a provisioning failure so
	// it surfaces from the first Publish/Subscribe rather than being swallowed.
	streamCfg *StreamConfig
	js        jetstream.JetStream
	jsErr     error

	mu     sync.Mutex
	subs   []*subscription
	closed bool
}

// Compile-time proof that *Bus satisfies the shared event interfaces. Adding the
// variadic durable knobs on the concrete SubscribeWith (not on Subscribe) keeps
// this satisfaction intact, so a value held as event.Subscriber — e.g. wired into
// cache.FromEventSubscriber — gets core-filter semantics unchanged.
var (
	_ event.Publisher  = (*Bus)(nil)
	_ event.Subscriber = (*Bus)(nil)
)

// subscription tracks a registered handler and its underlying subscription so it
// can be unsubscribed later. A core subscription carries natsSub; a durable
// JetStream subscription (created via SubscribeWith) carries jsConsume. Exactly
// one is non-nil for a live subscription.
type subscription struct {
	bus       *Bus
	natsSub   *nats.Subscription
	jsConsume jetstream.ConsumeContext
	opts      event.SubscribeOptions
}

// Unsubscribe cancels the NATS subscription and removes it from the bus.
// Calling Unsubscribe more than once is a no-op.
func (s *subscription) Unsubscribe() error {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()

	var err error
	switch {
	case s.jsConsume != nil:
		// Stopping the consume context halts delivery; a durable consumer's
		// server-side state is intentionally left in place so it survives.
		s.jsConsume.Stop()
		s.jsConsume = nil
	case s.natsSub != nil:
		err = s.natsSub.Unsubscribe()
		s.natsSub = nil
	default:
		return nil // already unsubscribed
	}
	for i, other := range s.bus.subs {
		if other == s {
			s.bus.subs = append(s.bus.subs[:i], s.bus.subs[i+1:]...)
			break
		}
	}
	if err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}
	return nil
}

// ConnectOption configures the Bus that Connect builds. It is an alias for the
// bus Option type, so Connect accepts every bus option — in particular
// WithJetStream, so a Connect-built bus pairs directly with durable mode:
//
//	bus, err := natsbus.Connect(url, natsbus.WithJetStream(cfg))
//
// The connection-level resilience defaults are baked in and are not customizable
// through Connect; a caller needing custom nats.Options should dial their own
// *nats.Conn with nats.Connect and pass it to New.
type ConnectOption = Option

// Connect dials a NATS connection at url with production-sane resilience
// defaults and returns a Bus that owns it (section 28.7.1): infinite reconnect
// so the client rides out broker outages, an 8 MiB reconnect buffer so publishes
// during a blip are retained rather than dropped, a bounded drain-on-Close, and
// disconnect/reconnect log callbacks. The returned bus is ready to pair with
// WithJetStream and any other bus option passed through opts.
//
// Because Connect owns the connection, Close drains and tears it down (unlike a
// New(conn) bus, whose connection the caller manages). New(conn) remains for
// callers who own their *nats.Conn and want to control dialing themselves. A
// dial failure is returned.
func Connect(url string, opts ...ConnectOption) (*Bus, error) {
	conn, err := nats.Connect(url, connectDialOptions()...)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", url, err)
	}
	return New(conn, opts...), nil
}

// connectDialOptions returns the production resilience options Connect dials
// with. The disconnect/reconnect callbacks log to stderr via the standard
// library, matching event.DefaultOnError's log.Printf sink; a consumer wanting
// a different sink dials their own connection and uses New.
func connectDialOptions() []nats.Option {
	return []nats.Option{
		nats.MaxReconnects(-1),
		nats.ReconnectBufSize(defaultReconnectBufSize),
		nats.DrainTimeout(defaultDrainTimeout),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Printf("natsbus: disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("natsbus: reconnected to %s", nc.ConnectedUrl())
		}),
	}
}

// New returns a Bus that publishes and subscribes using the given NATS
// connection. The connection is owned by the caller and must be closed
// separately after Close returns.
func New(conn *nats.Conn, opts ...Option) *Bus {
	b := &Bus{
		conn:   conn,
		prefix: DefaultPrefix,
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.streamCfg != nil {
		b.js, b.jsErr = b.provisionStream(*b.streamCfg)
	}
	return b
}

// provisionStream obtains a JetStream context and ensures the configured stream
// exists (create-or-update). The stream captures every subject under the bus
// prefix. It is called once from New when WithJetStream was passed; a returned
// error is stored on the Bus and surfaced from the first Publish/Subscribe.
func (b *Bus) provisionStream(cfg StreamConfig) (jetstream.JetStream, error) {
	js, err := jetstream.New(b.conn)
	if err != nil {
		return nil, fmt.Errorf("jetstream init: %w", err)
	}
	if _, err := js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name:       cfg.Name,
		Subjects:   []string{b.prefix + ".>"},
		MaxAge:     cfg.MaxAge,
		Duplicates: cfg.DedupWindow,
	}); err != nil {
		return nil, fmt.Errorf("provision stream %s: %w", cfg.Name, err)
	}
	return js, nil
}

// Publish serializes the event as JSON and publishes it to the subject
// derived from the event's schema and table. The JSON body is accompanied by
// out-of-band NATS headers (headerEnvelopeVersion always, headerTraceparent
// when Event.Metadata carries a "traceparent" value); the body itself is
// unchanged, so a consumer reading only msg.Data is unaffected. Returns an
// error if the bus is closed, the event cannot be serialized, or the NATS
// publish fails.
func (b *Bus) Publish(ctx context.Context, e event.Event) error {
	if b.isClosed() {
		return errors.New("publish: natsbus is closed")
	}
	if b.jsErr != nil {
		return fmt.Errorf("publish: jetstream unavailable: %w", b.jsErr)
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	subject := b.publishSubject(e.Schema, e.Table, e.Action)
	header := nats.Header{headerEnvelopeVersion: []string{b.envelopeVersion()}}
	// Lift trace context out of the envelope onto a wire header. The
	// "traceparent" value is captured by the producer's MetadataFunc at hook
	// entry (section 28.8); Metadata is how it survives the async
	// OnCommit deferral, the header is its wire home for WithContextPropagation.
	if tp := e.Metadata[headerTraceparent]; tp != "" {
		header.Set(headerTraceparent, tp)
	}
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  header,
	}
	// JetStream mode: publish to the persisted stream and stamp Nats-Msg-Id with
	// Event.ID so the broker's dedup window collapses duplicate publishes. Core
	// mode is unchanged — no dedup header, a plain fire-and-forget PublishMsg.
	if b.js != nil {
		msg.Header.Set(headerMsgID, e.ID)
		if _, err := b.js.PublishMsg(ctx, msg); err != nil {
			return fmt.Errorf("publish %s: %w", subject, err)
		}
		return nil
	}
	if err := b.conn.PublishMsg(msg); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

// PublishBatch publishes each event individually. NATS does not provide a
// native batch publish; callers that need batching should rely on the
// connection's buffered writes.
func (b *Bus) PublishBatch(ctx context.Context, events []event.Event) error {
	for i := range events {
		if err := b.Publish(ctx, events[i]); err != nil {
			return err
		}
	}
	return nil
}

// Subscribe registers a handler using core NATS delivery. The subscription
// subject is derived from opts.Tables: a single table subscribes to
// "{prefix}.*.{table}", multiple tables or no tables subscribe to "{prefix}.>"
// with handler-side filtering. opts.Group maps to a NATS queue group.
// opts.Actions are applied as a post-receive filter on the decoded event.
//
// Subscribe satisfies event.Subscriber, so its behavior is at-most-once even in
// JetStream mode: it is a real-time core tap on the subject, with no ack and no
// redelivery. For durable, at-least-once JetStream consumers with explicit
// acknowledgement use SubscribeWith.
func (b *Bus) Subscribe(opts event.SubscribeOptions, handler event.Handler) (event.Subscription, error) {
	return b.subscribeCore(opts, handler)
}

// SubscribeWith registers a handler with natsbus-local subscribe options. In
// JetStream mode (WithJetStream) it creates a durable consumer with explicit
// acknowledgement (section 28.7.1): a Handler returning nil acks the message
// and it is not redelivered; a non-nil error naks it (with WithBackoff delay, if
// set) so the broker redelivers it — at-least-once delivery. Handlers should
// therefore be idempotent. In core mode the JetStream-only options are inert and
// delivery is the same at-most-once core tap as Subscribe.
//
// The variadic lives on this concrete method only; event.Subscriber.Subscribe is
// frozen, so code holding the bus as event.Subscriber gets core-filter
// semantics unchanged while code holding *Bus gets the durable knobs.
func (b *Bus) SubscribeWith(opts event.SubscribeOptions, handler event.Handler, subOpts ...SubscribeOption) (event.Subscription, error) {
	if b.js == nil {
		// Core mode (or JetStream provisioning failed): the durable knobs have no
		// meaning; fall back to the core tap. subscribeCore surfaces jsErr and the
		// closed/nil-handler guards.
		return b.subscribeCore(opts, handler)
	}
	if err := b.subscribeGuard(handler); err != nil {
		return nil, err
	}
	var cfg subscribeConfig
	for _, o := range subOpts {
		o(&cfg)
	}
	return b.subscribeJetStream(opts, handler, cfg)
}

// subscribeGuard runs the shared subscribe preconditions: a non-nil handler, no
// pending JetStream provisioning error, and an open bus.
func (b *Bus) subscribeGuard(handler event.Handler) error {
	if handler == nil {
		return errors.New("subscribe: handler is nil")
	}
	if b.jsErr != nil {
		return fmt.Errorf("subscribe: jetstream unavailable: %w", b.jsErr)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errors.New("subscribe: natsbus is closed")
	}
	return nil
}

// trackSub records a live subscription for Close/Unsubscribe bookkeeping.
func (b *Bus) trackSub(sub *subscription) {
	b.mu.Lock()
	b.subs = append(b.subs, sub)
	b.mu.Unlock()
}

// subscribeCore registers a core NATS subscription (at-most-once). It backs both
// Subscribe and the core-mode fallback of SubscribeWith.
func (b *Bus) subscribeCore(opts event.SubscribeOptions, handler event.Handler) (event.Subscription, error) {
	if err := b.subscribeGuard(handler); err != nil {
		return nil, err
	}
	sub := &subscription{bus: b, opts: opts}
	subject := b.subscribeSubject(opts)
	natsSub, err := b.subscribeNATS(subject, opts.Group, b.msgHandler(opts, handler))
	if err != nil {
		return nil, fmt.Errorf("subscribe %s: %w", subject, err)
	}
	sub.natsSub = natsSub
	b.trackSub(sub)
	return sub, nil
}

// subscribeJetStream creates (or binds) a durable JetStream consumer over the
// bus's stream and drives the handler with explicit ack/nak. The consumer's
// FilterSubject matches the same subject a core Subscribe would use, so the
// opts.Tables single-table narrowing is enforced broker-side; multi-table and
// action filters are applied per-message via matches (a filtered-out message is
// acked so it is not redelivered). opts.Group maps to the durable name for
// load balancing when WithDurable is not given.
func (b *Bus) subscribeJetStream(opts event.SubscribeOptions, handler event.Handler, cfg subscribeConfig) (event.Subscription, error) {
	durable := cfg.durable
	if durable == "" {
		durable = opts.Group // empty when no group — an unnamed (ephemeral) consumer
	}
	consumerCfg := jetstream.ConsumerConfig{
		Durable:       durable,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: b.subscribeSubject(opts),
		// Delivery-start policy for replay/backfill. The zero value is
		// DeliverAllPolicy, so an un-set config is byte-identical to a consumer
		// with no start policy. OptStartTime/OptStartSeq are ignored by the
		// all/new policies.
		DeliverPolicy: cfg.deliverPolicy,
		OptStartTime:  cfg.optStartTime,
		OptStartSeq:   cfg.optStartSeq,
	}
	// A positive max-deliver caps broker redelivery; zero leaves it at the
	// JetStream default (unbounded). The final failed delivery is where the
	// dead-letter routing (if any) fires — see jsMsgHandler.
	if cfg.maxDeliver > 0 {
		consumerCfg.MaxDeliver = cfg.maxDeliver
	}
	cons, err := b.js.CreateOrUpdateConsumer(context.Background(), b.streamCfg.Name, consumerCfg)
	if err != nil {
		return nil, fmt.Errorf("subscribe consumer %s: %w", b.streamCfg.Name, err)
	}
	sub := &subscription{bus: b, opts: opts}
	consumeCtx, err := cons.Consume(b.jsMsgHandler(opts, handler, cfg))
	if err != nil {
		return nil, fmt.Errorf("consume %s: %w", b.streamCfg.Name, err)
	}
	sub.jsConsume = consumeCtx
	b.trackSub(sub)
	return sub, nil
}

// jsMsgHandler wraps the user handler for a JetStream consumer: it decodes the
// JSON body, applies the table/action filter, rebuilds a span-continuing ctx when
// a Propagator is set, then acknowledges per the error-as-Nak contract — nil
// acks, a non-nil error naks (with WithBackoff delay) for redelivery. A message
// that cannot be decoded is terminated (not redelivered forever); a message that
// fails the filter is acked. On the final permitted delivery (WithMaxDeliver) a
// failed message is instead routed to the WithDeadLetter subject and terminated.
// When a WithSubscribeErrorHandler is registered the handler error is also
// surfaced to it so each failed attempt is observable.
func (b *Bus) jsMsgHandler(opts event.SubscribeOptions, handler event.Handler, cfg subscribeConfig) jetstream.MessageHandler {
	return func(msg jetstream.Msg) {
		var e event.Event
		if err := json.Unmarshal(msg.Data(), &e); err != nil {
			_ = msg.Term()
			return
		}
		if !matches(opts, e) {
			_ = msg.Ack()
			return
		}
		ctx := context.Background()
		if b.propagator != nil {
			ctx = b.propagator.Extract(ctx, msg.Headers())
		}
		if err := handler(ctx, e); err != nil {
			if b.subErrHandler != nil {
				b.subErrHandler(ctx, e, err)
			}
			b.nakOrDeadLetter(msg, cfg)
			return
		}
		_ = msg.Ack()
	}
}

// nakOrDeadLetter decides the terminal action for a failed JetStream delivery.
// On the final permitted delivery — a positive WithMaxDeliver reached and a
// WithDeadLetter subject configured — it republishes the original envelope
// (headers intact) to the dead-letter subject and terminates the message so the
// broker stops redelivering it. Otherwise it naks (with the WithBackoff delay, if
// any) for another redelivery attempt. If the dead-letter publish fails the
// message is nak'd instead so it is not lost.
func (b *Bus) nakOrDeadLetter(msg jetstream.Msg, cfg subscribeConfig) {
	if cfg.deadLetter != "" && cfg.maxDeliver > 0 && isFinalDelivery(msg, cfg.maxDeliver) {
		if err := b.publishDeadLetter(cfg.deadLetter, msg); err == nil {
			_ = msg.Term()
			return
		}
		// Fall through to nak: routing failed, so keep the message for another try.
	}
	if cfg.backoff > 0 {
		_ = msg.NakWithDelay(cfg.backoff)
	} else {
		_ = msg.Nak()
	}
}

// isFinalDelivery reports whether this delivery is the last one max-deliver
// permits, i.e. the message would not be redelivered again after this attempt.
// A non-positive max-deliver has no bound (never final); a missing metadata read
// is treated as "not final" so the message is nak'd for a retry rather than
// prematurely dead-lettered.
func isFinalDelivery(msg jetstream.Msg, maxDeliver int) bool {
	if maxDeliver <= 0 {
		return false
	}
	md, err := msg.Metadata()
	if err != nil {
		return false
	}
	//nolint:gosec // G115: maxDeliver is guarded > 0 just above, so the conversion cannot wrap.
	return md.NumDelivered >= uint64(maxDeliver)
}

// publishDeadLetter republishes a failed message's original envelope and headers
// to the dead-letter subject over core NATS, so the payload, Event.ID
// (Nats-Msg-Id), traceparent, and envelope version all survive for downstream
// inspection. Core publish is used so the subject need not fall under the stream's
// captured subjects.
func (b *Bus) publishDeadLetter(subject string, msg jetstream.Msg) error {
	dl := &nats.Msg{
		Subject: subject,
		Data:    msg.Data(),
		Header:  msg.Headers(),
	}
	if err := b.conn.PublishMsg(dl); err != nil {
		return fmt.Errorf("dead-letter publish %s: %w", subject, err)
	}
	return nil
}

// Close drains the connection's pending messages and unsubscribes all
// handlers registered through this bus. The underlying *nats.Conn is not
// closed — callers manage its lifecycle.
func (b *Bus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	subs := b.subs
	b.subs = nil
	// Detach each subscription's handles under the lock, so a concurrent
	// Unsubscribe (which reads and nils the same fields under b.mu) cannot race
	// with this loop: whichever runs first nils the fields, and the other sees
	// nil and skips — each handle is stopped exactly once. The captured handles
	// are then stopped below, outside the lock, keeping the network calls off it.
	pending := make([]subscription, 0, len(subs))
	for _, s := range subs {
		pending = append(pending, subscription{jsConsume: s.jsConsume, natsSub: s.natsSub})
		s.jsConsume = nil
		s.natsSub = nil
	}
	b.mu.Unlock()

	var firstErr error
	for _, s := range pending {
		switch {
		case s.jsConsume != nil:
			s.jsConsume.Stop()
		case s.natsSub != nil:
			if err := s.natsSub.Unsubscribe(); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("unsubscribe: %w", err)
			}
		}
	}
	if err := b.conn.Drain(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("drain: %w", err)
	}
	return firstErr
}

// subject returns the NATS subject for a schema/table pair, omitting the
// schema segment when empty.
func (b *Bus) subject(schema, table string) string {
	if schema == "" {
		return fmt.Sprintf("%s.%s", b.prefix, table)
	}
	return fmt.Sprintf("%s.%s.%s", b.prefix, schema, table)
}

// publishSubject returns the subject an event is published to. With
// WithActionSubjects it appends the action segment ("{prefix}.{schema}.{table}.
// {action}") for broker-side action filtering; otherwise it is the plain
// schema/table subject.
func (b *Bus) publishSubject(schema, table string, action event.Action) string {
	base := b.subject(schema, table)
	if b.actionSubjects {
		return base + "." + string(action)
	}
	return base
}

// envelopeVersion returns the Sqlgen-Envelope-Version value stamped on published
// messages: the action-qualified subject scheme (WithActionSubjects) is a
// distinct wire scheme, so it stamps a bumped version.
func (b *Bus) envelopeVersion() string {
	if b.actionSubjects {
		return envelopeVersionActionSubjects
	}
	return envelopeVersion
}

// subscribeSubject returns the NATS subject used for a Subscribe call. A single
// table filter subscribes to "{prefix}.*.{table}" to cover all schemas;
// otherwise the wildcard "{prefix}.>" is used and tables are filtered in the
// handler. With WithActionSubjects a trailing action token is added: a single
// table plus a single action narrows to "{prefix}.*.{table}.{action}"
// (broker-side action filtering), a single table with any other action set uses
// "{prefix}.*.{table}.*", and the multi-table case stays "{prefix}.>" ('>'
// spans the extra action token).
func (b *Bus) subscribeSubject(opts event.SubscribeOptions) string {
	single := len(opts.Tables) == 1
	if !b.actionSubjects {
		if single {
			return fmt.Sprintf("%s.*.%s", b.prefix, opts.Tables[0])
		}
		return b.prefix + ".>"
	}
	if single {
		action := "*"
		if len(opts.Actions) == 1 {
			action = string(opts.Actions[0])
		}
		return fmt.Sprintf("%s.*.%s.%s", b.prefix, opts.Tables[0], action)
	}
	return b.prefix + ".>"
}

// msgHandler wraps the user handler with JSON decoding and post-receive
// table/action filtering. Messages that fail to decode or fail the filter are
// dropped silently. When WithContextPropagation is set, the ctx handed to the
// handler is rebuilt from the traceparent header so it continues the producing
// span; otherwise it is context.Background(). A handler error is retried per
// WithRetry and, if it still fails, routed to the WithSubscribeErrorHandler
// handler (section 28.5); with no error handler configured it is discarded
// without panic — core NATS cannot redeliver.
func (b *Bus) msgHandler(opts event.SubscribeOptions, handler event.Handler) nats.MsgHandler {
	return func(msg *nats.Msg) {
		var e event.Event
		if err := json.Unmarshal(msg.Data, &e); err != nil {
			return
		}
		if !matches(opts, e) {
			return
		}
		ctx := context.Background()
		if b.propagator != nil {
			ctx = b.propagator.Extract(ctx, msg.Header)
		}
		if err := b.deliver(ctx, e, handler); err != nil && b.subErrHandler != nil {
			b.subErrHandler(ctx, e, err)
		}
	}
}

// deliver invokes the handler, retrying up to the configured attempt count on
// core (at-most-once) subscriptions. It returns nil on the first success and
// the final error once the attempts are exhausted. Retry is a bounded
// in-process loop because core NATS cannot redeliver; JetStream redelivery
// (section 28.7.1) supersedes it in durable mode.
func (b *Bus) deliver(ctx context.Context, e event.Event, handler event.Handler) error {
	attempts := max(b.retryAttempts, 1)
	var err error
	for i := range attempts {
		if err = handler(ctx, e); err == nil {
			return nil
		}
		if i < attempts-1 && b.retryBackoff > 0 {
			time.Sleep(b.retryBackoff)
		}
	}
	return err
}

// subscribeNATS calls Subscribe or QueueSubscribe depending on whether a
// queue group was requested.
func (b *Bus) subscribeNATS(subject, group string, h nats.MsgHandler) (*nats.Subscription, error) {
	var (
		sub *nats.Subscription
		err error
	)
	if group == "" {
		sub, err = b.conn.Subscribe(subject, h)
	} else {
		sub, err = b.conn.QueueSubscribe(subject, group, h)
	}
	if err != nil {
		return nil, fmt.Errorf("nats subscribe: %w", err)
	}
	return sub, nil
}

// isClosed reports whether Close has been called.
func (b *Bus) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// matches reports whether an event satisfies the subscription's table and
// action filters. Empty filter lists match all values.
func matches(opts event.SubscribeOptions, e event.Event) bool {
	if len(opts.Tables) > 0 && !slices.Contains(opts.Tables, e.Table) {
		return false
	}
	if len(opts.Actions) > 0 && !slices.Contains(opts.Actions, e.Action) {
		return false
	}
	return true
}
