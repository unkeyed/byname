// The REST contract, mirrored from internal/httpapi/server.go (routes),
// internal/core (actors, roles and scopes) and internal/auth (keys,
// sessions).

type Brand<T, B extends string> = T & { readonly __brand: B };

export type KeyID = Brand<string, "KeyID">;

export type Email = string;

/** RFC 3339. */
export type Timestamp = string;

/** core.Scopes, in the order lists show them. */
export const scopes = ["results:read", "runs:write", "config:write"] as const;
export type Scope = (typeof scopes)[number];

export const scopeInfo: Record<Scope, { title: string; detail: string }> = {
  "results:read": { title: "Read results", detail: "Runs, answers, questions, brands, Search Console data and settings. Never secrets." },
  "runs:write": { title: "Run checks", detail: "Queue a check, and pause or resume the schedule." },
  "config:write": { title: "Change setup", detail: "Edit the brand, competitors, questions, surfaces and schedule." },
};

/** core.Roles. Admin and editor have every scope, viewer reads; only admins manage keys, apps and the team. */
export const roles = ["admin", "editor", "viewer"] as const;
export type Role = (typeof roles)[number];

export const roleInfo: Record<Role, string> = {
  admin: "Everything, including API keys, connected apps and the team",
  editor: "Reads results, runs checks and changes setup",
  viewer: "Reads results",
};

/** Who is calling. "oauth" is an MCP client acting for the person who approved it; its name reads "Claude Code (via Alex Rivera)". */
export interface Actor {
  via: "session" | "api_key" | "oauth";
  id: string;
  user_id: number;
  name: string;
  role: Role;
  /** What the caller may do now. */
  scopes: Scope[];
}

export interface Status {
  version: string;
  /** Whether someone has finished setting up the brand, surfaces and questions. */
  onboarded: boolean;
}

export interface Key {
  id: KeyID;
  name: string;
  /** "byn_ab12", so people can tell keys apart. */
  prefix: string;
  /** What it was granted. A request gets only what its creator's role still has. */
  scopes: Scope[];
  /** When it stops working. Absent for a key that works until revoked. */
  expires_at?: Timestamp;
  created_at: Timestamp;
  last_used_at?: Timestamp;
}

export interface KeyOptions {
  name: string;
  scopes: Scope[];
  expires_at?: Timestamp;
}

/** An MCP client a person approved. Its name is whatever the app registered, unverified. */
export interface ConnectedApp {
  id: string;
  name: string;
  /** The name, or else the email, of the person it acts as. */
  approved_by: string;
  scopes: Scope[];
  created_at: Timestamp;
  last_used_at?: Timestamp;
}

/** A pending OAuth authorization, for the consent page. */
export interface ConsentRequest {
  client_name: string;
  /** Where the code goes on approval. */
  redirect_uri: string;
  client_registered_at: Timestamp;
  /** The scopes to choose from: what the app asked for, or all of them. */
  scopes: Scope[];
}

/** A person who can sign into the UI. */
export interface User {
  id: number;
  email: Email;
  name: string;
  role: Role;
  created_at: Timestamp;
}

export interface Invitation {
  email: Email;
  role: Role;
}

/** POST /v1/keys: the plaintext key is returned exactly once. */
export interface CreatedKey extends Key {
  key: string;
}

export type ProviderID = "google" | "github" | "oidc";

export interface SignInProvider {
  id: ProviderID;
  name: string;
  /** The redirect URI to register at the provider. */
  callback_url: string;
}

export interface AuthOptions {
  providers: SignInProvider[];
  password_login: boolean;
  /** True while nobody can sign in; the server log prints a one-time setup link. */
  setup_required: boolean;
}

export type SecondFactor = "totp" | "recovery_code" | "passkey";

/** Every sign-in step ends in a session, or in a pending login (byname_pending cookie) that owes a second factor. */
export type SignInResponse = { status: "signed_in" } | { status: "second_factor"; second_factors: SecondFactor[] };

/** A WebAuthn ceremony: options carry binary fields as base64url. */
export interface CeremonyStart<O> {
  handle: string;
  options: { publicKey: O };
}
export type AssertionStart = CeremonyStart<PublicKeyCredentialRequestOptionsJSON>;
export type RegistrationStart = CeremonyStart<PublicKeyCredentialCreationOptionsJSON>;

export interface CeremonyFinish {
  handle: string;
  /** The PublicKeyCredential as JSON, binary fields base64url. */
  credential: object;
  name?: string;
}

export interface Passkey {
  /** The credential ID, base64url. */
  id: string;
  name: string;
  created_at: Timestamp;
  last_used_at: Timestamp | null;
}

export interface LinkedIdentity {
  provider: string;
  email: Email;
  created_at: Timestamp;
  last_used_at: Timestamp | null;
}

export interface Session {
  id: number;
  /** The factors that opened it, joined with "+": "password+totp", "github". */
  method: string;
  user_agent: string;
  created_at: Timestamp;
  last_seen_at: Timestamp;
  current: boolean;
}

export interface Account {
  user: User;
  passkeys: Passkey[];
  identities: LinkedIdentity[];
  totp: boolean;
  recovery_codes_left: number;
  password: boolean;
  /** Signed in within 15 minutes, so sign-in methods may change without confirming again. */
  recent: boolean;
  sessions: Session[];
}

export interface TOTPEnrolment {
  handle: string;
  /** Base32, for typing into an app by hand. */
  secret: string;
  uri: string;
  /** The otpauth URI as a PNG data URL. */
  qr: string;
}

/** engines.Surface: where people ask. Stored keys never change once shipped. */
export type SurfaceKey = string;

/** How close an answer is to what a person sees; lines of different fidelity are never combined. */
export type Fidelity = "app" | "model_match" | "approximation";

export interface SelfBrand {
  name: string;
  aliases: string[];
  /** The first is the primary domain; links to it and its subdomains count as citations. */
  domains: string[];
}

/** How your own name is found: whole word and case-sensitive, or also anywhere ignoring case. */
export type MatchMode = "whole_word" | "self_anywhere";

/** PUT /v1/settings takes all of it at once. */
export interface Settings {
  brand: SelfBrand;
  match_mode: MatchMode;
  /** ISO 3166-1 alpha-2, such as "US". */
  market: string;
  /** ISO 639-1, such as "en". */
  language: string;
  /** IANA. */
  time_zone: string;
  check_every_days: number;
  /** "HH:MM" in time_zone. */
  check_time: string;
  spend_cap_run_usd: number;
  spend_cap_month_usd: number | null;
  concurrency: number;
  retries: number;
  timeout_s: number;
  /** null keeps raw responses forever. */
  raw_retention_days: number | null;
}

export interface SettingsView extends Settings {
  checks_paused: boolean;
  next_check_at: Timestamp | null;
  /** Set when a scheduled check is claimed, even one that was skipped. */
  last_check_at: Timestamp | null;
  updated_at: Timestamp;
  /** Lowercased; a Search Console query containing one is brand. */
  branded_terms: string[];
  /** True while the terms are the brand's name and aliases. */
  branded_terms_default: boolean;
}

export type BrandKind = "self" | "competitor" | "technology";

export interface BrandEntry {
  id: number;
  name: string;
  aliases: string[];
  domains: string[];
  kind: BrandKind;
}

/** POST /v1/brands upserts by name, ignoring case. Your own brand is set through settings. */
export interface BrandInput {
  name: string;
  aliases: string[];
  domains: string[];
  kind: Exclude<BrandKind, "self">;
}

export interface Question {
  id: number;
  group: string;
  text: string;
}

export interface QuestionSet {
  id: number;
  version: number;
  notes: string;
  created_at: Timestamp;
  questions: Question[];
  /** How this version carries on the previous one's questions. */
  carried: { from: number; to: number }[];
  retired: number[];
}

export interface QuestionSetSummary {
  id: number;
  version: number;
  notes: string;
  created_at: Timestamp;
  questions: number;
}

/**
 * A question matching a current one ignoring case and punctuation carries it
 * on by itself; every other current question must be in some replaces or in
 * retired.
 */
export interface QuestionsInput {
  notes: string;
  questions: { group: string; text: string; replaces: number[] }[];
  retired: number[];
}

/** How a surface is asked: through Treg, a scraping provider's own key, a model provider's key, or an OpenAI-compatible gateway. */
export type Route = "treg" | "scraper" | "provider" | "gateway";

export type CredentialKind = "treg" | "openai" | "anthropic" | "perplexity" | "google" | "dataforseo" | "cloro" | "serpapi" | "gateway";

/** Whether web search can be forced on a route: the app decides, forceable, only turned on, always on, or per model. */
export type SearchControl = "by_app" | "forceable" | "enable_only" | "always" | "by_model";

export interface RouteOption {
  route: Route;
  /** The key it calls with; "treg" for the Treg route. */
  credential: CredentialKind;
  provider: string;
  /** The label a pair gets on this route before any test. */
  fidelity: Fidelity;
  takes_model: boolean;
  search: SearchControl;
}

export type SurfaceGroup = "apps" | "developer_tools" | "approximations";

export interface Pair {
  id: number;
  surface: SurfaceKey;
  /** Empty on app surfaces, where the app picks the model. */
  model: string;
  fidelity: Fidelity;
  force_web_search: boolean;
  enabled: boolean;
  paused_reason: string;
  last_tested_at: Timestamp | null;
  /** "" untested, "ok", or why the test failed. */
  test_status: string;
  test_searched: boolean | null;
}

export interface Surface {
  key: SurfaceKey;
  title: string;
  group: SurfaceGroup;
  /** The endpoint id answers on the current route store. */
  endpoint: string;
  fidelity: Fidelity;
  takes_model: boolean;
  search: SearchControl;
  route: Route;
  credential: CredentialKind;
  /** Gateway routes only. */
  base_url: string;
  /** Every route this surface can take, Treg first. */
  routes: RouteOption[];
  enabled: boolean;
  /** Ticked at the start of onboarding. */
  default_on: boolean;
  pairs: Pair[];
}

export interface SurfaceInput {
  enabled: boolean;
  /** Omit to keep the current route. Changing it resets every pair's test. */
  route?: Route;
  base_url?: string;
  /** Omit to keep; at least one when enabling a surface that takes a model. */
  models?: { model: string; force_web_search?: boolean }[];
}

export interface ModelOption {
  name: string;
  web_search: boolean;
}

export interface PairTest {
  pair: Pair;
  status: AnswerStatus;
  error: string;
  cost_usd: number | null;
  /** False on token-billed APIs that report no cost. */
  cost_reported: boolean;
  searched: boolean;
  cited: boolean;
  citations: number;
  reported_model: string;
}

/** Admin only. No API ever returns the secret. */
export interface Credential {
  kind: CredentialKind;
  title: string;
  /** What to paste, such as "login:password from DataForSEO's API dashboard". */
  hint: string;
  set: boolean;
  /** Treg personal tokens only. */
  org: string;
  last_tested_at: Timestamp | null;
  /** "ok", or why it failed, or that the provider has no free test call. */
  last_test_status: string;
  updated_at: Timestamp | null;
  used_by: SurfaceKey[];
}

export type StepKey = "brand" | "competitors" | "questions" | "surfaces" | "search_console" | "schedule" | "review";

export interface OnboardingStep {
  key: StepKey;
  title: string;
  saved: boolean;
  required: boolean;
}

export interface Onboarding {
  steps: OnboardingStep[];
  finished_at: Timestamp | null;
  /** What still blocks finishing, in plain words; empty when Finish will work. */
  missing: string[];
}

export interface EstimateRow {
  pair_id: number;
  surface: SurfaceKey;
  title: string;
  model: string;
  fidelity: Fidelity;
  route: Route;
  endpoint: string;
  calls: number;
  cost_per_call_usd: number | null;
  /** history: the mean of this endpoint's past answers; sample: the cost sampled on Nov 21, 2026. */
  source: "history" | "sample" | "unknown";
  /** Answers averaged, for history. */
  samples: number;
  cost_usd: number | null;
}

export interface Estimate {
  questions: number;
  pairs: number;
  /** Questions times active pairs. */
  calls: number;
  /** Over the pairs with a cost per call. */
  known_usd: number;
  unknown_pairs: number;
  rows: EstimateRow[];
}

export type RunStatus = "queued" | "running" | "completed" | "capped" | "refused" | "failed";

export interface EndpointStats {
  endpoint: string;
  calls: number;
  succeeded: number;
  success_rate: number;
  cost_usd: number;
  /** Answered calls that reported no cost: unknown, not $0. */
  missing_cost: number;
  cost_per_call: number | null;
}

export interface Run {
  id: number;
  status: RunStatus;
  trigger: string;
  question_set_id: number;
  question_set_version: number;
  queued_at: Timestamp;
  started_at: Timestamp | null;
  finished_at: Timestamp | null;
  spend_usd: number;
  spend_missing_calls: number;
  calls: number;
  succeeded: number;
  mentioned: number;
  cited: number;
  error: string;
  refusal_code: string;
  endpoints: EndpointStats[];
}

export interface Job {
  id: number;
  job: string;
  trigger: string;
  run_id: number | null;
  status: string;
  started_at: Timestamp;
  finished_at: Timestamp | null;
  error: string;
  spend_usd: number | null;
}

export interface PairRef {
  /** 0 on a line that combines a surface's models. */
  id: number;
  surface: SurfaceKey;
  model: string;
}

export interface QuestionRef {
  id: number;
  group: string;
  text: string;
}

/** One line: a pair (or a surface) on one endpoint at one fidelity. */
export interface Series {
  pair: PairRef;
  endpoint: string;
  fidelity: Fidelity;
}

export interface RatePoint {
  run_id: number;
  run_started_at: Timestamp;
  calls: number;
  answered: number;
  mentioned: number;
  cited: number;
  mention_rate: number | null;
  citation_rate: number | null;
}

export interface RateSeries extends Series {
  models: string[];
  points: RatePoint[];
}

export interface RunSummary {
  run_id: number;
  run_started_at: Timestamp;
  answered: number;
  mentioned: number;
  cited: number;
  mention_rate: number | null;
  citation_rate: number | null;
  mentions: number;
  share: number | null;
}

export interface BrandStanding {
  brand_id: number;
  name: string;
  kind: BrandKind;
  mentions: number;
  share: number;
  /** 0 for a brand never named. */
  rank: number;
}

export interface HeatColumn extends Series {
  models: string[];
  answered: number;
  /** Only brands this line named; a missing brand was named 0 times. */
  cells: { brand_id: number; named: number; rate: number }[];
}

export interface Competitors {
  runs: number;
  answered: number;
  mentions: number;
  brands_named: number;
  brands: BrandStanding[];
  columns: HeatColumn[];
}

export type AnswerStatus = "ok" | "no_answer" | "failed";
export type Outcome = "gained" | "lost" | "moved" | "unchanged" | "not_comparable";

export interface Cell {
  answer_id: number;
  status: AnswerStatus;
  mentioned: boolean;
  cited: boolean;
  position: number | null;
  outcome: Outcome;
}

export interface QuestionRow {
  question: QuestionRef;
  /** Lines up with the table's columns; null where the pair has no answer. */
  cells: (Cell | null)[];
  answered: number;
  named: number;
  cited: number;
  best_position: number | null;
  gained: number;
  lost: number;
  moved: number;
}

export interface QuestionTable {
  run_id: number;
  previous_run_id: number | null;
  columns: Series[];
  rows: QuestionRow[];
}

export interface Standing {
  answer_id: number;
  status: AnswerStatus;
  mentioned: boolean;
  cited: boolean;
  position: number | null;
}

export interface PairChange {
  question_id: number;
  pair_id: number;
  question: QuestionRef;
  pair: PairRef;
  outcome: Outcome;
  before: Standing | null;
  after: Standing | null;
}

export interface Comparison {
  before: number;
  after: number;
  counts: Record<Outcome, number>;
  net: number;
  pairs: PairChange[];
}

export interface HistoryPoint {
  run_id: number;
  run_started_at: Timestamp;
  answer_id: number;
  question_id: number;
  status: AnswerStatus;
  mentioned: boolean;
  cited: boolean;
  position: number | null;
  reported_model: string;
  /** The change since this series' previous run, such as "Gained mention at #1 · Northwind cited". */
  summary: string;
}

export interface HistorySeries extends Series {
  answered: number;
  named: number;
  cited: number;
  best_position: number | null;
  points: HistoryPoint[];
}

export interface History {
  question_id: number;
  series: HistorySeries[];
}

export interface Mention {
  brand_id: number;
  name: string;
  kind: BrandKind;
  position: number | null;
}

export interface AnswerSummary {
  id: number;
  run_id: number;
  question: QuestionRef;
  pair: PairRef;
  endpoint: string;
  fidelity: Fidelity;
  reported_model: string;
  status: AnswerStatus;
  error: string;
  mentioned: boolean;
  cited: boolean;
  position: number | null;
  cost_usd: number | null;
  citation_count: number;
  mentions: Mention[];
  answered_at: Timestamp;
}

/** What Byname found on a cited page the last time it read it. */
export interface PageCheck {
  /** disallowed: robots.txt said no; refused: not http(s), or a private address. */
  status: "ok" | "failed" | "disallowed" | "refused";
  fetched_at: Timestamp;
  /** Null unless status is ok. Read from the served HTML, so a page built in the browser can read as absent. */
  self_present: boolean | null;
  brands: string[];
  competitors: string[];
  error: string;
}

export interface Answer extends AnswerSummary {
  text: string;
  fan_out: string[] | null;
  /** page is null until Byname has fetched the cited page. */
  citations: { url: string; domain: string; is_self: boolean; page: PageCheck | null }[];
}

export interface GapPair extends Series {
  answer_id: number;
  status: AnswerStatus;
  named: boolean;
  position: number | null;
  /** The web searches the engine ran; empty where the endpoint does not expose them. */
  fan_out: string[] | null;
}

export interface Gap {
  question: QuestionRef;
  /** How many pairs answered. These are surface and model pairs, not engines. */
  engines: number;
  named: number;
  competitors: number;
  opportunity: number;
  /** Monthly searches; null while no demand source is set up. */
  demand: number | null;
  named_on_miss: string[];
  pairs: GapPair[];
}

export interface RunRef {
  id: number;
  started_at: Timestamp;
}

export type DotState = "named" | "cited" | "not_named" | "failed";

export interface Dot {
  run_id: number;
  state: DotState;
  named: number;
  answered: number;
}

export interface PromptRow {
  question: QuestionRef;
  answered: number;
  named_by: Series[];
  best_position: number | null;
  instead: string[];
  /** Lines up with Prompts.runs; null where the question was not asked. */
  dots: (Dot | null)[];
}

export interface Prompts {
  runs: RunRef[];
  latest_run_id: number;
  rows: PromptRow[];
}

export interface SourcePage {
  url: string;
  domain: string;
  is_self: boolean;
  citations: number;
  answers: number;
  check: PageCheck | null;
  outreach: boolean;
}

export interface SourceDomain {
  domain: string;
  is_self: boolean;
  citations: number;
  answers: number;
  pages: number;
  outreach: number;
}

export interface Sources {
  answered: number;
  citing: number;
  citations: number;
  domain_count: number;
  page_count: number;
  domains: SourceDomain[];
  pages: SourcePage[];
}

/** One chart line: a surface (pair id 0) or one of its pairs, on one endpoint at one fidelity. */
export interface Line {
  surface: SurfaceKey;
  pair: number;
  endpoint: string;
  fidelity: Fidelity;
}

export interface EnginePrompt {
  question: QuestionRef;
  answer_id: number;
  position: number | null;
  cited: boolean;
  instead: string[];
}

export interface Engine extends Series {
  models: string[];
  runs: number;
  latest_run_id: number;
  answered: number;
  named: number;
  visibility: number | null;
  prompts: number;
  visible_prompts: number;
  average_position: number | null;
  points: RatePoint[];
  ranking: BrandStanding[];
  not_mentioned: EnginePrompt[];
  visible: EnginePrompt[];
}

/** YYYY-MM-DD. */
export type Day = string;

export interface SearchTotals {
  clicks: number;
  impressions: number;
  /** Clicks over impressions, 0 to 1. */
  ctr: number;
  /** Average position, weighted by impressions; 0 with no impressions. */
  position: number;
}

export interface SearchPeriod extends SearchTotals {
  from: Day;
  to: Day;
  brand_clicks: number;
  /** Anonymized queries are never non-brand. */
  non_brand_clicks: number;
  anonymized_clicks: number;
  days_with_data: number;
}

/** The latest 28 days, ending on the latest final day, and the 28 before. */
export interface SearchSummary {
  property: string;
  current: SearchPeriod | null;
  previous: SearchPeriod | null;
}

export interface SearchDay extends SearchTotals {
  date: Day;
  /** False while Google still revises the day. */
  final: boolean;
}

export interface SearchDaily {
  property: string;
  latest_final_day: Day;
  /** Oldest first; a day with no row had no impressions. */
  days: SearchDay[];
}

/** One trailing 28-day window, named by its last day. */
export interface BrandPoint {
  date: Day;
  clicks: number;
  brand_clicks: number;
  non_brand_clicks: number;
  anonymized_clicks: number;
}

export interface BrandSeries {
  property: string;
  terms: string[];
  points: BrandPoint[];
}

export interface PageRow extends SearchTotals {
  page: string;
}

export interface QueryRow extends SearchTotals {
  query: string;
  branded: boolean;
}

export interface TopList {
  property: string;
  from: Day;
  to: Day;
}

export const goalMetrics = ["clicks", "impressions", "non_brand_clicks"] as const;
export type GoalMetric = (typeof goalMetrics)[number];
export const goalWindows = [28, 90] as const;
export type GoalWindow = (typeof goalWindows)[number];

export interface GoalInput {
  metric: GoalMetric;
  window_days: GoalWindow;
  target: number;
  due_on: Day;
}

export interface GoalProgress {
  from: Day;
  to: Day;
  value: number;
  /** Value over target; 1 or more once reached. */
  fraction: number;
  reached: boolean;
  days_with_data: number;
}

export interface Goal extends GoalInput {
  id: number;
  /** Null until Search Console data is synced. */
  progress: GoalProgress | null;
}

export type GscMethod = "oauth" | "service_account";
export type PropertyKind = "domain" | "url_prefix";

/** GET /v1/gsc: the connection and the data stored. Data can outlive a disconnect. */
export interface GscStatus {
  connected: boolean;
  /** The instance has GSC_GOOGLE_CLIENT_ID, which Sign in with Google needs. */
  oauth_available: boolean;
  method: GscMethod | "";
  /** The service account's email; empty for a Google sign-in. */
  account: string;
  property: string;
  property_kind: PropertyKind | "";
  backfill: boolean;
  connected_at: Timestamp | null;
  last_sync_at: Timestamp | null;
  last_sync_error: string;
  syncing: boolean;
  /** The property the stored numbers are for; empty when nothing is stored. */
  data_property: string;
  first_day: Day;
  /** The latest day Google has finalized, which every window ends on. */
  latest_final_day: Day;
  /** Stored days Google still revises. */
  partial_days: Day[];
}

export interface GscProperty {
  site_url: string;
  permission_level: string;
  kind: PropertyKind;
  /** Matches the brand's primary domain. */
  suggested: boolean;
}

/** Which pairs a results read covers, and since when. */
export interface ResultsFilter {
  since?: Timestamp;
  surface?: SurfaceKey;
  model?: string;
}

export type Grouping = "pair" | "surface";

/** internal/notify's registry of event kinds. */
export type EventKindName =
  | "check_completed"
  | "check_refused"
  | "check_failed"
  | "spend_cap_reached"
  | "pair_paused"
  | "goal_reached"
  | "search_moved"
  | "change_verdict"
  | "token_expiring";

export interface EventKind {
  kind: EventKindName;
  title: string;
  description: string;
  /** Who sees its alerts in the app: anyone who reads results, or admins only. */
  audience: "results:read" | "admin";
}

/** One event as the app shows it. path is the page it links to. */
export interface Alert {
  id: number;
  kind: EventKindName;
  title: string;
  lines: string[];
  path: string;
  payload: unknown;
  created_at: Timestamp;
  read: boolean;
}

export interface AlertList {
  alerts: Alert[];
  unread: number;
}

/** email_api is Resend's HTTP API, the one email API Byname speaks. */
export type ChannelKind = "smtp" | "email_api" | "slack_webhook" | "webhook";

export type SMTPTLS = "starttls" | "tls" | "none";

/** What a channel shows in the clear, per kind. Secrets never come back. */
export interface ChannelSettings {
  host?: string;
  port?: number;
  tls?: SMTPTLS;
  username?: string;
  from?: string;
  to?: string[];
  url?: string;
}

export interface NotificationChannel {
  id: number;
  name: string;
  kind: ChannelKind;
  settings: ChannelSettings;
  events: EventKindName[];
  enabled: boolean;
  last_test_at: Timestamp | null;
  /** "ok", or why the last test failed. Empty until tested. */
  last_test_status: string;
  created_at: Timestamp;
  updated_at: Timestamp;
}

/** On an edit, secrets names only those to change. kind cannot change. */
export interface ChannelInput {
  name: string;
  kind: ChannelKind;
  settings: ChannelSettings;
  secrets?: { password?: string; api_key?: string; webhook_url?: string };
  events: EventKindName[];
  enabled?: boolean;
}

/** POST /v1/notification-channels: a webhook's signing secret, shown this once. */
export interface CreatedChannel extends NotificationChannel {
  signing_secret?: string;
}

export interface Delivery {
  id: number;
  alert_id: number;
  kind: EventKindName;
  title: string;
  channel_id: number;
  channel_name: string;
  channel_kind: ChannelKind;
  status: "pending" | "sent" | "failed";
  attempts: number;
  error: string;
  sent_at: Timestamp | null;
  next_attempt_at: Timestamp | null;
  created_at: Timestamp;
}

/**
 * Error body for every non-2xx: {"error": "<message>"}, plus request_id on a
 * 5xx and code on a 403 that wants a fresh sign-in.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code?: "reauth_required";
  constructor(status: number, message: string, code?: "reauth_required") {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export const needsReauth = (err: unknown) => err instanceof ApiError && err.code === "reauth_required";

async function request<T>(method: string, path: string, opts: { body?: unknown } = {}): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: "same-origin",
    headers: opts.body === undefined ? undefined : { "Content-Type": "application/json" },
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  });
  if (!res.ok) {
    let message = res.statusText || `HTTP ${res.status}`;
    let code: "reauth_required" | undefined;
    try {
      const body: unknown = await res.json();
      if (body && typeof body === "object" && "error" in body && typeof body.error === "string") message = body.error;
      if (body && typeof body === "object" && "code" in body && body.code === "reauth_required") code = body.code;
    } catch {
      // Not JSON (a proxy error page, say); keep the status text.
    }
    throw new ApiError(res.status, message, code);
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

const enc = encodeURIComponent;

function qs(params: Record<string, string | number | undefined>): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== "") q.set(k, String(v));
  const s = q.toString();
  return s ? `?${s}` : "";
}

export const api = {
  auth: {
    options: () => request<AuthOptions>("GET", "/auth/options"),
    /** Usernameless: the passkey names the account. A passkey needs no second factor. */
    passkeyBegin: () => request<AssertionStart>("POST", "/auth/passkey/begin"),
    passkeyFinish: (body: CeremonyFinish) => request<SignInResponse>("POST", "/auth/passkey/finish", { body }),
    /** Only with PASSWORD_LOGIN on. Never a session alone: success asks for a second factor. */
    password: (email: string, password: string) => request<SignInResponse>("POST", "/auth/password", { body: { email, password } }),
    secondFactor: () => request<SignInResponse>("GET", "/auth/second-factor"),
    /** An authenticator code or a recovery code. A wrong code leaves the login pending. */
    secondFactorCode: (code: string) => request<SignInResponse>("POST", "/auth/second-factor/code", { body: { code } }),
    secondFactorPasskeyBegin: () => request<AssertionStart>("POST", "/auth/second-factor/passkey/begin"),
    secondFactorPasskeyFinish: (body: CeremonyFinish) => request<SignInResponse>("POST", "/auth/second-factor/passkey/finish", { body }),
    /** 404 once the link is used, expired, or never existed. recovery is true for a link from `byname recover`. */
    checkSetup: (token: string) => request<{ recovery: boolean }>("GET", `/auth/setup/${enc(token)}`),
    invite: (token: string) => request<Invitation>("GET", `/auth/invites/${enc(token)}`),
    signupBegin: (body: { setup_token: string; email: string; name: string } | { invite_token: string; name: string }) =>
      request<RegistrationStart>("POST", "/auth/signup/passkey/begin", { body }),
    /** Creates the account with its passkey, uses up the link, and signs in. */
    signupFinish: (body: CeremonyFinish) => request<SignInResponse>("POST", "/auth/signup/passkey/finish", { body }),
    logout: () => request<void>("POST", "/auth/logout"),
  },

  me: {
    get: () => request<Actor>("GET", "/v1/me"),
  },

  /** Every change to a sign-in method can fail with reauth_required (see needsReauth). */
  account: {
    get: () => request<Account>("GET", "/v1/account"),
    addPasskeyBegin: () => request<RegistrationStart>("POST", "/v1/account/passkeys/begin"),
    addPasskey: (body: CeremonyFinish) => request<Passkey>("POST", "/v1/account/passkeys", { body }),
    removePasskey: (id: string) => request<void>("DELETE", `/v1/account/passkeys/${enc(id)}`),
    unlink: (provider: string) => request<void>("DELETE", `/v1/account/identities/${enc(provider)}`),
    totpBegin: () => request<TOTPEnrolment>("POST", "/v1/account/totp/begin"),
    /** Returns 10 recovery codes, shown once. */
    totpFinish: (handle: string, code: string) =>
      request<{ recovery_codes: string[] }>("POST", "/v1/account/totp", { body: { handle, code } }),
    totpDisable: () => request<void>("DELETE", "/v1/account/totp"),
    regenerateRecoveryCodes: () => request<{ recovery_codes: string[] }>("POST", "/v1/account/recovery-codes"),
    setPassword: (password: string) => request<void>("PUT", "/v1/account/password", { body: { password } }),
    removePassword: () => request<void>("DELETE", "/v1/account/password"),
    revokeSession: (id: number) => request<void>("DELETE", `/v1/account/sessions/${id}`),
    reauthPasskeyBegin: () => request<AssertionStart>("POST", "/v1/account/reauth/passkey/begin"),
    reauthPasskey: (body: CeremonyFinish) => request<void>("POST", "/v1/account/reauth/passkey", { body }),
    reauthCode: (code: string) => request<void>("POST", "/v1/account/reauth/code", { body: { code } }),
  },

  status: () => request<Status>("GET", "/v1/status"),

  keys: {
    list: () => request<Key[]>("GET", "/v1/keys"),
    create: (body: KeyOptions) => request<CreatedKey>("POST", "/v1/keys", { body }),
    revoke: (id: KeyID) => request<void>("DELETE", `/v1/keys/${enc(id)}`),
  },

  apps: {
    list: () => request<ConnectedApp[]>("GET", "/v1/apps"),
    revoke: (id: string) => request<void>("DELETE", `/v1/apps/${enc(id)}`),
  },

  consent: {
    get: (handle: string) => request<ConsentRequest>("GET", `/v1/oauth/requests/${enc(handle)}`),
    /** Returns the URL to send the browser to: the app's redirect with a code, or with error=access_denied. */
    decide: (handle: string, approve: boolean, scopes: Scope[]) =>
      request<{ redirect: string }>("POST", `/v1/oauth/requests/${enc(handle)}`, { body: { approve, scopes } }),
  },

  users: {
    /** Everyone who can sign in, oldest first. */
    list: () => request<User[]>("GET", "/v1/users"),
    /** The last admin cannot step down (409). */
    setRole: (id: number, role: Role) => request<void>("PATCH", `/v1/users/${id}`, { body: { role } }),
    /** Ends their sessions too. The last admin cannot be removed (409). */
    remove: (id: number) => request<void>("DELETE", `/v1/users/${id}`),
  },

  settings: {
    get: () => request<SettingsView>("GET", "/v1/settings"),
    put: (body: Settings) => request<SettingsView>("PUT", "/v1/settings", { body }),
  },
  brands: {
    /** Your own brand first, then by name. */
    list: () => request<BrandEntry[]>("GET", "/v1/brands"),
    upsert: (body: BrandInput) => request<BrandEntry>("POST", "/v1/brands", { body }),
    update: (id: number, body: BrandInput) => request<BrandEntry>("PUT", `/v1/brands/${id}`, { body }),
    remove: (id: number) => request<void>("DELETE", `/v1/brands/${id}`),
  },
  questions: {
    /** 404 before the first set. */
    current: () => request<QuestionSet>("GET", "/v1/questions"),
    put: (body: QuestionsInput) => request<QuestionSet>("PUT", "/v1/questions", { body }),
    /** Newest first. */
    versions: () => request<QuestionSetSummary[]>("GET", "/v1/question-sets"),
  },
  surfaces: {
    list: () => request<Surface[]>("GET", "/v1/surfaces"),
    put: (key: SurfaceKey, body: SurfaceInput) => request<Surface>("PUT", `/v1/surfaces/${enc(key)}`, { body }),
    /** The live list on a route (the current one by default). 400 where the app picks the model, or the route's key is not set. */
    models: (key: SurfaceKey, route?: Route, baseURL?: string) =>
      request<ModelOption[]>("GET", `/v1/surfaces/${enc(key)}/models${qs({ route, base_url: baseURL })}`),
  },
  pairs: {
    /** One short call through the pair's route; confirms or lowers its fidelity. */
    test: (id: number) => request<PairTest>("POST", `/v1/pairs/${id}/test`),
  },
  /** Admin only; setting and removing need a recent sign-in. */
  credentials: {
    list: () => request<Credential[]>("GET", "/v1/credentials"),
    set: (kind: CredentialKind, secret: string, org?: string) => request<Credential>("PUT", `/v1/credentials/${kind}`, { body: { secret, org } }),
    test: (kind: CredentialKind) => request<Credential>("POST", `/v1/credentials/${kind}/test`),
    /** 409 while a surface uses it. */
    remove: (kind: CredentialKind) => request<void>("DELETE", `/v1/credentials/${kind}`),
  },
  checks: {
    pause: () => request<SettingsView>("POST", "/v1/checks/pause"),
    resume: () => request<SettingsView>("POST", "/v1/checks/resume"),
  },
  onboarding: {
    get: () => request<Onboarding>("GET", "/v1/onboarding"),
    /** After a step's own data saved, or on Skip for an optional step. */
    markSaved: (step: StepKey) => request<Onboarding>("PUT", `/v1/onboarding/steps/${step}`),
    /** 400 naming what is missing. Sets the first scheduled check. */
    finish: () => request<Onboarding>("POST", "/v1/onboarding/finish"),
  },
  estimate: () => request<Estimate>("GET", "/v1/estimate"),

  runs: {
    /** Newest first. */
    list: (limit = 50) => request<Run[]>("GET", `/v1/runs${qs({ limit })}`),
    get: (id: number) => request<Run>("GET", `/v1/runs/${id}`),
    /** 201 with a new run, or 200 with the run already queued or running. Poll get() for progress. */
    runNow: () => request<Run>("POST", "/v1/runs"),
    questions: (id: number, f: ResultsFilter) => request<QuestionTable>("GET", `/v1/runs/${id}/questions${qs({ surface: f.surface, model: f.model })}`),
    answers: (id: number) => request<AnswerSummary[]>("GET", `/v1/runs/${id}/answers`),
  },
  jobs: (limit = 20) => request<Job[]>("GET", `/v1/jobs${qs({ limit })}`),
  answer: (id: number) => request<Answer>("GET", `/v1/answers/${id}`),
  compare: (before: number, after: number) => request<Comparison>("GET", `/v1/compare${qs({ before, after })}`),
  history: (question: number, since?: Timestamp) => request<History>("GET", `/v1/questions/${question}/history${qs({ since })}`),
  gaps: (run?: number, f: ResultsFilter = {}) => request<{ run_id: number; gaps: Gap[] }>("GET", `/v1/gaps${qs({ run, surface: f.surface, model: f.model })}`),
  prompts: (f: ResultsFilter) => request<Prompts>("GET", `/v1/prompts${qs({ ...f })}`),
  sources: (f: ResultsFilter) => request<Sources>("GET", `/v1/sources${qs({ ...f })}`),
  engine: (l: Line, since?: Timestamp) =>
    request<Engine>("GET", `/v1/metrics/engine${qs({ since, surface: l.surface, pair: l.pair || undefined, endpoint: l.endpoint, fidelity: l.fidelity })}`),

  metrics: {
    rates: (f: ResultsFilter, by: Grouping) => request<RateSeries[]>("GET", `/v1/metrics/rates${qs({ ...f, by })}`),
    summary: (f: ResultsFilter) => request<RunSummary[]>("GET", `/v1/metrics/summary${qs({ ...f })}`),
    competitors: (f: ResultsFilter, by: Grouping) => request<Competitors>("GET", `/v1/metrics/competitors${qs({ ...f, by })}`),
  },

  search: {
    summary: () => request<SearchSummary>("GET", "/v1/search/summary"),
    /** Defaults to the 90 days up to the latest stored day. */
    daily: (from?: Day) => request<SearchDaily>("GET", `/v1/search/daily${qs({ from })}`),
    brand: (from?: Day) => request<BrandSeries>("GET", `/v1/search/brand${qs({ from })}`),
    pages: (limit: number) => request<TopList & { pages: PageRow[] }>("GET", `/v1/search/pages${qs({ limit })}`),
    queries: (limit: number, nonBrand: boolean) =>
      request<TopList & { non_brand: boolean; queries: QueryRow[] }>("GET", `/v1/search/queries${qs({ limit, non_brand: nonBrand ? "true" : undefined })}`),
  },

  goals: {
    list: () => request<Goal[]>("GET", "/v1/goals"),
    /** Upserts on metric, window and due date. */
    create: (body: GoalInput) => request<Goal>("POST", "/v1/goals", { body }),
    update: (id: number, body: GoalInput) => request<Goal>("PUT", `/v1/goals/${id}`, { body }),
    remove: (id: number) => request<void>("DELETE", `/v1/goals/${id}`),
  },

  /** An empty list goes back to the brand's name and aliases. */
  setBrandedTerms: (terms: string[]) => request<Settings>("PUT", "/v1/settings/branded-terms", { body: { terms } }),

  /** Everything but status is admin-only; connecting can fail with reauth_required. */
  gsc: {
    status: () => request<GscStatus>("GET", "/v1/gsc"),
    properties: () => request<GscProperty[]>("GET", "/v1/gsc/properties"),
    setProperty: (property: string, backfill: boolean) => request<GscStatus>("PUT", "/v1/gsc/property", { body: { property, backfill } }),
    serviceAccount: (key: string) => request<GscStatus>("PUT", "/v1/gsc/service-account", { body: { key } }),
    /** Google's consent page; it sends the browser back to /auth/gsc/callback. */
    oauthStart: () => request<{ url: string }>("POST", "/v1/gsc/oauth/start"),
    /** Keeps the synced data. */
    disconnect: () => request<void>("DELETE", "/v1/gsc"),
  },

  invites: {
    /** Returns a one-time token valid for 7 days; the UI builds the link. */
    create: (email: Email, role: Role) => request<{ token: string }>("POST", "/v1/invites", { body: { email, role } }),
  },

  alerts: {
    /** Newest first, with the caller's read state and unread count. */
    list: (opts: { unread?: boolean; limit?: number } = {}) => request<AlertList>("GET", `/v1/alerts${qs({ unread: opts.unread ? "true" : undefined, limit: opts.limit })}`),
    read: (body: { ids: number[] } | { all: true }) => request<{ unread: number }>("POST", "/v1/alerts/read", { body }),
  },

  /** Admin only. Creating and editing need a recent sign-in (see needsReauth). */
  channels: {
    list: () => request<{ channels: NotificationChannel[]; events: EventKind[] }>("GET", "/v1/notification-channels"),
    create: (body: ChannelInput) => request<CreatedChannel>("POST", "/v1/notification-channels", { body }),
    update: (id: number, body: ChannelInput) => request<NotificationChannel>("PUT", `/v1/notification-channels/${id}`, { body }),
    remove: (id: number) => request<void>("DELETE", `/v1/notification-channels/${id}`),
    /** A failed test is a result: last_test_status says why. */
    test: (id: number) => request<NotificationChannel>("POST", `/v1/notification-channels/${id}/test`),
    deliveries: (channel?: number) => request<Delivery[]>("GET", `/v1/notification-deliveries${qs({ channel, limit: 50 })}`),
  },
};

export function inviteURL(token: string): string {
  return `${location.origin}/invite/${enc(token)}`;
}

export type OAuthStart = { intent: "login"; next: string } | { intent: "link" | "reauth" } | { intent: "setup" | "invite"; token: string };

/** A full-page navigation, not a fetch: the provider redirects back with ?error= on failure. */
export function oauthURL(provider: string, start: OAuthStart): string {
  return `/auth/oauth/${enc(provider)}/start?${new URLSearchParams(start)}`;
}
