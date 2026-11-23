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
  "results:read": { title: "Read results", detail: "Checks, answers, questions, brands, Search Console data and settings. Never secrets." },
  "runs:write": { title: "Start checks", detail: "Start a check, and pause or resume the schedule." },
  "config:write": { title: "Change setup", detail: "Edit the brand, competitors, questions, AI apps and schedule." },
};

/** core.Roles. Admin and editor have every scope, viewer reads; only admins manage keys, apps and the team. */
export const roles = ["admin", "editor", "viewer"] as const;
export type Role = (typeof roles)[number];

/** How a role is written in the app. The API's values stay lower case. */
export const roleLabel: Record<Role, string> = { admin: "Admin", editor: "Editor", viewer: "Viewer" };

export const roleInfo: Record<Role, string> = {
  admin: "Everything, including API keys, connected apps and the team",
  editor: "Reads results, starts checks and changes setup",
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

/** An unknown, used or expired invite answers live false. */
export type Invitation = { live: true; email: Email; role: Role } | { live: false };

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

/** A scheduled check in the instance's own time zone, so the app can say "tomorrow at 06:00, then every 3 days". */
export interface UpcomingCheck {
  at: Timestamp;
  /** YYYY-MM-DD in the time zone. */
  local_date: string;
  /** HH:MM in the time zone. */
  local_time: string;
  /** Calendar days from today in the time zone: 0 is today, 1 tomorrow. */
  days_away: number;
  then_every_days: number;
}

export interface SettingsView extends Settings {
  checks_paused: boolean;
  next_check_at: Timestamp | null;
  /** Set when a scheduled check is claimed, even one that was skipped. */
  last_check_at: Timestamp | null;
  /** The latest check, whatever started it. */
  last_check: {
    run_id: number;
    trigger: "scheduled" | "run_now";
    status: string;
    queued_at: Timestamp;
    started_at: Timestamp | null;
    finished_at: Timestamp | null;
  } | null;
  /** The next scheduled check in the instance's time zone; null while paused or before onboarding is finished. */
  next_check: UpcomingCheck | null;
  updated_at: Timestamp;
  onboarded_at: Timestamp | null;
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
  /** The text names your brand, by the rules answers are scored with. */
  names_brand: boolean;
  /** False by default when the text names your brand; set with questions.setScoring. */
  count_in_scores: boolean;
}

/** POST /v1/naming-check: what text being typed names, before anything is saved. */
export interface NamingCheck {
  /** Your brand's name; empty before it is set. */
  brand: string;
  questions: { text: string; names_brand: boolean; brands: string[] }[];
  aliases: { alias: string; common_word: boolean }[];
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

export type StepKey = "brand" | "competitors" | "questions" | "surfaces" | "search_console" | "social" | "schedule" | "review";

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
  /** The scheduled check finishing now would set; null once finished. */
  first_scheduled_check: UpcomingCheck | null;
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
  /** Over the pairs with a cost per call, plus reading the answers for other brands when that is on. */
  known_usd: number;
  unknown_pairs: number;
  rows: EstimateRow[];
  /** Reading each answer for brands you don't track; null while off or with no Treg token. */
  find_brands: { answers: number; cost_per_answer_usd: number; cost_usd: number } | null;
}

/** GET /v1/costs: every cost figure, in US dollars. Expected figures are estimates. */
export interface Costs {
  /** One answer on each surface, on its current route, in onboarding order. */
  per_answer: {
    surface: SurfaceKey;
    title: string;
    group: SurfaceGroup;
    route: Route;
    endpoint: string;
    enabled: boolean;
    default_on: boolean;
    models: string[];
    cost_per_answer_usd: number | null;
    source: "history" | "sample" | "unknown";
    samples: number;
  }[];
  checks: {
    check: Estimate;
    check_every_days: number;
    paused: boolean;
    /** 0 while paused. */
    checks_per_month: number;
    month_usd: number;
  };
  social: {
    enabled: boolean;
    fetches_per_month: number;
    terms: number;
    /** 0 without a Treg token. */
    treg_networks: number;
    treg_per_fetch_usd: number;
    /** What the Treg networks cost a month at these settings, on or off. */
    treg_month_usd: number;
    bluesky_on: boolean;
    bluesky_month_usd: number;
    /** 0 while social is off. */
    month_usd: number;
  };
  other_brands: OtherBrandsSettings;
  /** checks.month_usd plus social.month_usd. */
  month_estimate_usd: number;
  /** The monthly cap finishing onboarding sets when none is set. */
  default_month_cap_usd: number;
  month: {
    start: Timestamp;
    checks_usd: number;
    social_usd: number;
    /** Reading answers for other brands and looking up logos. */
    brands_usd: number;
    spent_usd: number;
    missing_cost_calls: number;
    cap_usd: number | null;
  };
}

export type RunStatus = "queued" | "running" | "completed" | "capped" | "refused" | "failed";

export interface EndpointStats {
  endpoint: string;
  calls: number;
  succeeded: number;
  success_rate: number;
  cost_usd: number;
  /** Answered calls that reported no cost: unknown, not $0.00. */
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
  /** Answers that did not fail and count in scores; mentioned and cited are out of these. */
  scored: number;
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
  /** False when its answers are left out of scores, as for a question that names your brand. Its answers are still shown. */
  counts_in_scores: boolean;
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
  /** The question as it reads now. */
  question: QuestionRef;
  series: HistorySeries[];
}

export interface Mention {
  brand_id: number;
  name: string;
  kind: BrandKind;
  position: number | null;
}

/** A brand an answer named that you don't track. It counts in no score. */
export interface OtherMention {
  brand_id: number;
  name: string;
}

/** GET /v1/other-brands: the brands answers in range named that you don't track, most named first. */
export interface OtherBrands {
  answered: number;
  /** Of answered, how many were read for other brands. */
  read: number;
  latest_run: number;
  /** 0 when latest_run is the first check. */
  previous_run: number;
  hidden: number;
  /** A brand is listed once answers to at least this many different questions offered it as a choice. */
  min_questions: number;
  brands: OtherBrand[];
}

export interface OtherBrand {
  brand_id: number;
  name: string;
  domain: string;
  named: number;
  latest: number;
  previous: number;
  /** Null when the latest check is the first. */
  change: number | null;
  questions: { question_id: number; text: string; named: number }[];
}

export interface HiddenBrand {
  brand_id: number;
  name: string;
  domain: string;
  named: number;
}

export interface OtherBrandsSettings {
  has_token: boolean;
  onboarded: boolean;
  find: { on: boolean; cost_per_answer_usd: number; model: string; min_questions: number };
  logos: { on: boolean; cost_per_brand_usd: number; month_limit: number; month_max_usd: number; this_month: number; logos: number };
}

export interface OtherBrandsSettingsInput {
  find?: boolean;
  logos?: boolean;
  logo_month_limit?: number;
  min_questions?: number;
}

export interface BrandBackfill {
  unread: number;
  cost_per_answer_usd: number;
  cost_usd: number;
  ready: boolean;
  latest: { job_id: number; status: RunStatus | "queued"; started_at: Timestamp; finished_at: Timestamp | null; read: number; spent_usd: number; error: string } | null;
}

/** A brand with a stored logo. Draw it from logoURL. */
export interface BrandLogo {
  brand_id: number;
  domain: string;
  color: string;
  version: number;
}

export const logoURL = (l: BrandLogo) => `/v1/brands/${l.brand_id}/logo?v=${l.version}`;

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
  /** Brands the answer named that you don't track, in answer order. */
  others: OtherMention[];
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
  /** The pages the answer linked to, in order, without your own site's. */
  links: string[];
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
  /** Pages marked as answering this question, under any version of it, most recently marked first. */
  pages: MarkedPage[];
}

/** A page marked as answering a question, as its missed question row shows it. */
export interface MarkedPage {
  id: number;
  url: string;
  added_at: Timestamp;
  /** Answers since it was marked that link to it, to any question. */
  linked: number;
}

/** How one check's answers to a question named you. */
export interface Naming {
  run_id: number;
  run_started_at: Timestamp;
  /** Answers that did not fail. */
  answered: number;
  named: number;
}

/** A marked page and what happened since it was marked. */
export interface HandledPage {
  id: number;
  url: string;
  /** The question it answers, as it reads now. */
  question: QuestionRef;
  added_at: Timestamp;
  /** Who marked it; empty for an API key or a person since removed. */
  added_by: string;
  /** Every answer since it was marked that links to it, to any question, from any AI app. */
  linked: { answers: number; questions: number; pairs: PairRef[]; first_run_id: number | null; first_at: Timestamp | null; latest_run_id: number | null };
  /** The question's last check before the mark; null when there was none. */
  before: Naming | null;
  /** The question's latest check since the mark; null until one runs. */
  now: Naming | null;
}

export type LineState = "named_linked" | "named" | "linked" | "missed";

export interface BriefLine extends Series {
  /** "Claude Code · claude-sonnet-4-5", or "ChatGPT app" for a surface that takes no model. */
  title: string;
  answer_id: number;
  /** linked: linked to your site without naming you. */
  state: LineState;
  position: number | null;
  /** Your brand and competitors the answer named, in the order it named them. */
  brands: string[];
  fan_out: string[];
  links: string[];
}

export interface BriefPage {
  url: string;
  domain: string;
  is_self: boolean;
  answers: number;
  check: PageCheck | null;
  outreach: boolean;
}

/** One question on one check, as a prompt for an agent that writes your site's content. */
export interface Brief {
  run_id: number;
  question: QuestionRef;
  answered: number;
  named: number;
  linked: number;
  instead: string[];
  fan_out: string[];
  /** Those that left you out first. */
  lines: BriefLine[];
  pages: BriefPage[];
  /** Markdown to paste into an agent working on your site. */
  prompt: string;
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

/** internal/changes: what happened to the page. */
export type ChangeKind = "new_page" | "edit" | "removed";

/** Where a change is in its life, derived from what is stored. */
export type ChangeStage = "awaiting_crawl" | "awaiting_interim" | "awaiting_verdict" | "decided";

/** What a read concluded. */
export type ChangeOutcome = "improved" | "worsened" | "no_change" | "insufficient_data";

export type ChangeMetric = "position" | "ctr" | "impressions";

/** How far a page must move against the control pages for a metric to count. */
export interface ChangeThresholds {
  /** Places of average position. */
  position: number;
  /** Percentage points of CTR. */
  ctr_points: number;
  /** Impressions ratio against the controls; its inverse counts as worse. */
  impressions_ratio: number;
}

export interface ChangeSettings extends ChangeThresholds {
  /** Days after the first crawl for the interim read, 7 to 90. */
  interim_days: number;
  /** Days after the first crawl for the final read, 14 to 180 and more than interim_days. */
  verdict_days: number;
}

/** One side of a read: the page's totals and the control set's over the same final days. */
export interface ChangeWindow {
  from: Day;
  to: Day;
  days: number;
  page: SearchTotals;
  control: SearchTotals;
}

/** Differences in differences: the page's move minus the control set's. */
export interface ChangeDeltas {
  /** In places; negative means the page moved up. */
  position: number;
  /** In percentage points. */
  ctr_points: number;
  /** Page after/before over controls after/before; above 1 means it gained on them. */
  impressions_ratio: number;
}

export interface ChangeRead {
  kind: "interim" | "final";
  /** The day the read was taken, in the instance's time zone. */
  read_on: Day;
  outcome: ChangeOutcome;
  /** Why the data was insufficient; empty otherwise. */
  reason: string;
  before: ChangeWindow;
  after: ChangeWindow;
  deltas: ChangeDeltas | null;
  signals: { metric: ChangeMetric; way: "improved" | "worsened" }[] | null;
  controls: string[] | null;
  thresholds: ChangeThresholds;
}

/** One page changed once, with where it is in the watch. */
export interface Change {
  id: number;
  url: string;
  kind: ChangeKind;
  source: "github" | "manual";
  /** owner/name; empty for a manual change. */
  repo: string;
  /** 0 for a manual change. */
  pr_number: number;
  pr_title: string;
  pr_url: string;
  merged_at: Timestamp;
  notes: string;
  stage: ChangeStage;
  /** Google's first crawl seen after the merge. */
  first_crawl_at: Timestamp | null;
  /** Days in the instance's time zone, null until the first crawl. */
  interim_on: Day | null;
  verdict_on: Day | null;
  /** The page over the verdict-length window ending the day before the merge; null until those days are final. */
  before: (SearchTotals & { from: Day; to: Day }) | null;
  interim: ChangeRead | null;
  final: ChangeRead | null;
  verdict: ChangeOutcome | null;
}

export type ChangeStepKind = "merged" | "inspected" | "crawled" | "interim" | "verdict";

export interface ChangeStep {
  kind: ChangeStepKind;
  /** RFC 3339, or a day for interim and verdict (in the future while due). */
  at: string;
  done: boolean;
  detail: string;
}

export interface UrlInspection {
  checked_at: Timestamp;
  /** Google's verdict: PASS, PARTIAL, FAIL or NEUTRAL. */
  verdict: string;
  coverage_state: string;
  last_crawl_at: Timestamp | null;
  /** Why Google refused the inspection; empty when it answered. */
  error: string;
}

export interface ChangeDetail extends Change {
  timeline: ChangeStep[];
  /** Newest first. */
  inspections: UrlInspection[];
}

/** A change logged by hand. merged_at is RFC 3339, or a day meaning midnight in the instance's time zone. */
export interface ManualChange {
  url: string;
  kind: ChangeKind;
  merged_at: string;
  notes?: string;
}

/** One merge to mark on a daily chart, on its Search Console (Pacific) day. */
export interface ChangeMarker {
  date: Day;
  label: string;
  change_ids: number[];
}

/** A page held unchanged as the verdicts' baseline. */
export interface ControlPage {
  id: number;
  url: string;
  /** The last day it counts as a control. */
  hold_until: Day;
  note: string;
  /** False once hold_until has passed in the instance's time zone. */
  active: boolean;
}

export interface ControlInput {
  url: string;
  hold_until: Day;
  note?: string;
}

/** A changed file whose repo path matches glob is the page url, with {1}, {2} replaced by what each wildcard matched. */
export interface PathRule {
  glob: string;
  url: string;
}

/** The repo connection. It never carries the token. */
export interface RepoStatus {
  /** A token is stored. */
  connected: boolean;
  /** Empty before a repo was ever connected. */
  provider: "github" | "";
  /** owner/name. */
  repo: string;
  default_branch: string;
  /** Null for a token without expiry. */
  token_expires_at: Timestamp | null;
  /** The token expires within 14 days. */
  expiring_soon: boolean;
  rules: PathRule[];
  connected_at: Timestamp | null;
  last_poll_at: Timestamp | null;
  last_poll_error: string;
}

/** internal/social: the networks searched for posts that mention you. */
export type SocialNetwork = "x" | "reddit" | "hackernews" | "linkedin" | "youtube" | "threads" | "bluesky";

/** A count the network does not give is null, never 0. */
export interface Engagement {
  likes: number | null;
  replies: number | null;
  reposts: number | null;
  /** Points on Hacker News, score on Reddit. */
  score: number | null;
  /** Views on X and YouTube. */
  views: number | null;
  /** Ranks posts within a network: views on YouTube, elsewhere the other counts added up. */
  total: number;
}

export interface SocialCall {
  id: number;
  route: "treg" | "direct";
  treg_call_id: string;
  /** Null when Treg reported no cost; 0 for a direct call, which is free. */
  cost_usd: number | null;
  cost_missing: boolean;
}

export interface SocialMention {
  id: number;
  network: SocialNetwork;
  post_id: string;
  url: string;
  author_handle: string;
  author_name: string;
  text: string;
  posted_at: Timestamp;
  /** The network gave only an age, as YouTube does. */
  posted_approx: boolean;
  engagement: Engagement;
  term: string;
  first_seen_at: Timestamp;
  hidden: boolean;
  /** The left-out label hiding it, empty when shown. */
  left_out_as: string;
  call: SocialCall | null;
}

export interface SocialMentionList {
  mentions: SocialMention[];
  /** Shown mentions that match, which can be more than returned. */
  total: number;
  /** Matching mentions by left-out accounts. */
  hidden: number;
}

export interface SocialMentionQuery {
  network?: SocialNetwork;
  since?: string;
  include_hidden?: boolean;
  limit?: number;
}

export interface SocialDigest {
  cadence: "daily" | "weekly";
  from: Day;
  to: Day;
  starts_at: Timestamp;
  ends_at: Timestamp;
  finished: boolean;
  mentions: number;
  hidden: number;
  networks: { network: SocialNetwork; title: string; mentions: number; top: SocialMention[] }[];
  new_authors: { network: SocialNetwork; handle: string; name: string; profile: string; mentions: number }[];
  calls: number;
  spend_usd: number;
  /** Treg calls that reported no cost, left out of spend_usd. */
  spend_missing_calls: number;
}

export interface SocialNetworkStatus {
  network: SocialNetwork;
  title: string;
  enabled: boolean;
  route: "treg" | "direct";
  tool: string;
  cost_per_call_usd: number;
  last_call_at: Timestamp | null;
  last_error: string;
  /** The latest call found the network's public search asking for sign-in. */
  unavailable: boolean;
}

export interface SocialSettings {
  /** Off until someone turns it on; nothing runs before onboarding is finished either way. */
  enabled: boolean;
  terms: string[];
  default_terms: string[];
  custom_terms: boolean;
  fetch_every_hours: number;
  digest_cadence: "daily" | "weekly";
  /** 0 is Sunday. */
  digest_weekday: number;
  time_zone: string;
  token_set: boolean;
  networks: SocialNetworkStatus[];
  last_fetch_at: Timestamp | null;
  next_fetch_at: Timestamp | null;
}

export interface SocialSettingsInput {
  /** Omit to keep the current setting. */
  enabled?: boolean;
  /** [] searches for the brand's name and primary domain. */
  terms: string[];
  fetch_every_hours: number;
  digest_cadence: "daily" | "weekly";
  digest_weekday: number;
  networks?: { network: SocialNetwork; enabled: boolean }[];
}

export interface LeftOut {
  id: number;
  network: SocialNetwork;
  handle: string;
  label: string;
  profile: string;
  mentions: number;
}

export interface LeftOutInput {
  network: SocialNetwork;
  handle: string;
  label: string;
}

/** Which pairs a results read covers, and since when. */
export interface ResultsFilter {
  since?: Timestamp;
  surface?: SurfaceKey;
  model?: string;
}

export type Grouping = "pair" | "surface";

/** How one question moved across its AI apps between two checks. */
export interface QuestionChange {
  question_id: number;
  question: string;
  gained: number;
  lost: number;
  moved: number;
}

/** A finished check against the previous one, the counts GET /v1/compare returns. previous_run_id is 0 for the first check. */
export interface CheckCompletedPayload {
  run_id: number;
  previous_run_id: number;
  gained: number;
  lost: number;
  moved: number;
  unchanged: number;
  not_comparable: number;
  net: number;
  /** named of answered is the headline: answers that named you, of those that did not fail. */
  named: number;
  answered: number;
  spend_usd: number;
  /** The questions that changed most, at most three. */
  top: QuestionChange[];
}

export interface CheckRefusedPayload {
  run_id: number;
  /** Treg's error code, such as insufficient_balance. */
  code: string;
  message: string;
}

export interface CheckFailedPayload {
  run_id: number;
  message: string;
}

export interface SpendCapReachedPayload {
  run_id: number;
  spend_usd: number;
  message: string;
}

export interface PairPausedPayload {
  pair_id: number;
  surface: SurfaceKey;
  model: string;
  reason: string;
}

export interface GoalReachedPayload {
  goal_id: number;
  metric: GoalMetric;
  window_days: number;
  target: number;
  value: number;
  due_on: Day;
}

export interface SearchMovedPayload {
  metric: GoalMetric;
  window_days: number;
  before: number;
  after: number;
}

export interface ChangeVerdictPayload {
  change_id: number;
  url: string;
  verdict: ChangeOutcome;
  /** The result in one line. */
  detail: string;
}

export interface TokenExpiringPayload {
  provider: string;
  repo: string;
  expires_at: Timestamp;
}

export interface SocialDigestPost {
  /** The author as the network writes them, such as @dana or u/dana. */
  author: string;
  /** The start of the post. */
  text: string;
  url: string;
  engagement: number;
}

export interface SocialDigestPayload {
  cadence: "daily" | "weekly";
  from: Day;
  to: Day;
  mentions: number;
  /** Posts by left-out accounts, not counted in mentions. */
  hidden: number;
  networks: { network: SocialNetwork; title: string; mentions: number; top: SocialDigestPost[] }[];
  /** At most ten of the new_authors_count accounts whose first mention fell in the period. */
  new_authors: string[];
  new_authors_count: number;
  spend_usd: number;
  spend_missing_calls: number;
}

export interface PageLinkedPayload {
  page_id: number;
  url: string;
  question_text: string;
  /** The AI apps whose answers linked to it, by name. */
  apps: string[];
  /** That check's answers that linked to it, to any question. */
  answers: number;
}

/** Each event kind in internal/notify's registry and the payload it carries. */
export interface EventPayloads {
  check_completed: CheckCompletedPayload;
  check_refused: CheckRefusedPayload;
  check_failed: CheckFailedPayload;
  spend_cap_reached: SpendCapReachedPayload;
  pair_paused: PairPausedPayload;
  goal_reached: GoalReachedPayload;
  search_moved: SearchMovedPayload;
  change_verdict: ChangeVerdictPayload;
  token_expiring: TokenExpiringPayload;
  social_digest: SocialDigestPayload;
  page_linked: PageLinkedPayload;
}

export type EventKindName = keyof EventPayloads;

export interface EventKind {
  kind: EventKindName;
  title: string;
  description: string;
  /** Who sees its notifications in the app: anyone who reads results, or admins only. */
  audience: "results:read" | "admin";
}

/** One event as the app shows it, which the API calls an alert. path is the page it links to; lines are the text email and Slack send. */
export type Alert = {
  [K in EventKindName]: {
    id: number;
    kind: K;
    title: string;
    lines: string[];
    path: string;
    payload: EventPayloads[K];
    created_at: Timestamp;
    read: boolean;
  };
}[EventKindName];

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
    /** true counts its answers in scores, false leaves them out, null goes back to the default. Current set only. */
    setScoring: (id: number, count: boolean | null) => request<Question>("PUT", `/v1/questions/${id}/scoring`, { body: { count_in_scores: count } }),
  },
  /** Which brands question texts name, and which aliases are everyday words. Stores nothing. */
  namingCheck: (body: { questions?: string[]; aliases?: string[] }) => request<NamingCheck>("POST", "/v1/naming-check", { body }),
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
  costs: () => request<Costs>("GET", "/v1/costs"),

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
  brief: (question: number, run?: number, f: ResultsFilter = {}) => request<Brief>("GET", `/v1/questions/${question}/brief${qs({ run, surface: f.surface, model: f.model })}`),
  gaps: (run?: number, f: ResultsFilter = {}) => request<{ run_id: number; gaps: Gap[] }>("GET", `/v1/gaps${qs({ run, surface: f.surface, model: f.model })}`),
  handled: {
    list: () => request<{ pages: HandledPage[] }>("GET", "/v1/handled"),
    /** Marking the same page again keeps the first mark. */
    mark: (question: number, url: string) => request<{ id: number; question_id: number; url: string; added_at: Timestamp }>("POST", `/v1/questions/${question}/pages`, { body: { url } }),
    unmark: (id: number) => request<void>("DELETE", `/v1/question-pages/${id}`),
  },
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
  setBrandedTerms: (terms: string[]) => request<SettingsView>("PUT", "/v1/settings/branded-terms", { body: { terms } }),

  /** Everything but status is admin-only; connecting can fail with reauth_required. */
  gsc: {
    status: () => request<GscStatus>("GET", "/v1/gsc"),
    properties: () => request<GscProperty[]>("GET", "/v1/gsc/properties"),
    setProperty: (property: string, backfill: boolean) => request<GscStatus>("PUT", "/v1/gsc/property", { body: { property, backfill } }),
    serviceAccount: (key: string) => request<GscStatus>("PUT", "/v1/gsc/service-account", { body: { key } }),
    /** Google's consent page. The callback returns the browser to the page named by returnTo, with ?connected=google or ?error=. */
    oauthStart: (returnTo: "settings" | "onboarding" = "settings") => request<{ url: string }>("POST", "/v1/gsc/oauth/start", { body: { return_to: returnTo } }),
    /** Keeps the synced data. */
    disconnect: () => request<void>("DELETE", "/v1/gsc"),
  },

  changes: {
    /** The latest merge first. */
    list: () => request<Change[]>("GET", "/v1/changes"),
    get: (id: number) => request<ChangeDetail>("GET", `/v1/changes/${id}`),
    log: (body: ManualChange) => request<ChangeDetail>("POST", "/v1/changes", { body }),
    /** Manual changes only; one from GitHub is 409. */
    remove: (id: number) => request<void>("DELETE", `/v1/changes/${id}`),
    /** Search Console days, at most 500 apart, oldest first. */
    markers: (from: Day, to: Day) => request<ChangeMarker[]>("GET", `/v1/changes/markers${qs({ from, to })}`),
    settings: () => request<ChangeSettings>("GET", "/v1/changes/settings"),
    putSettings: (body: ChangeSettings) => request<ChangeSettings>("PUT", "/v1/changes/settings", { body }),
  },
  controlPages: {
    /** By URL. */
    list: () => request<ControlPage[]>("GET", "/v1/control-pages"),
    /** Upserts on URL. */
    upsert: (body: ControlInput) => request<ControlPage>("POST", "/v1/control-pages", { body }),
    remove: (id: number) => request<void>("DELETE", `/v1/control-pages/${id}`),
  },
  /** Connecting, testing and disconnecting are admin-only; connecting can fail with reauth_required. */
  repo: {
    get: () => request<RepoStatus>("GET", "/v1/repo"),
    connect: (repo: string, token: string) => request<RepoStatus>("PUT", "/v1/repo", { body: { repo, token } }),
    test: () => request<RepoStatus>("POST", "/v1/repo/test"),
    /** Keeps the repo, its rules and the changes found. */
    disconnect: () => request<void>("DELETE", "/v1/repo"),
    /** 404 before a repo was ever connected. */
    putRules: (rules: PathRule[]) => request<RepoStatus>("PUT", "/v1/repo/rules", { body: { rules } }),
  },

  otherBrands: {
    list: (f: ResultsFilter) => request<OtherBrands>("GET", `/v1/other-brands${qs({ ...f })}`),
    hidden: () => request<HiddenBrand[]>("GET", "/v1/other-brands/hidden"),
    /** Adds it as a competitor; past answers are scored again. */
    track: (id: number) => request<BrandEntry>("POST", `/v1/other-brands/${id}/track`),
    hide: (id: number) => request<void>("POST", `/v1/other-brands/${id}/hide`),
    unhide: (id: number) => request<void>("POST", `/v1/other-brands/${id}/unhide`),
    settings: () => request<OtherBrandsSettings>("GET", "/v1/other-brands/settings"),
    putSettings: (body: OtherBrandsSettingsInput) => request<OtherBrandsSettings>("PUT", "/v1/other-brands/settings", { body }),
    backfill: () => request<BrandBackfill>("GET", "/v1/other-brands/backfill"),
    /** Needs runs:write; spends money. */
    startBackfill: () => request<BrandBackfill>("POST", "/v1/other-brands/backfill"),
  },
  logos: () => request<BrandLogo[]>("GET", "/v1/logos"),

  social: {
    mentions: (q: SocialMentionQuery = {}) =>
      request<SocialMentionList>("GET", `/v1/social/mentions${qs({ network: q.network, since: q.since, include_hidden: q.include_hidden ? "true" : undefined, limit: q.limit })}`),
    /** Without a day, the latest period that has ended. */
    digest: (day?: Day) => request<SocialDigest>("GET", `/v1/social/digest${qs({ day })}`),
    settings: () => request<SocialSettings>("GET", "/v1/social/settings"),
    putSettings: (body: SocialSettingsInput) => request<SocialSettings>("PUT", "/v1/social/settings", { body }),
    leftOut: () => request<LeftOut[]>("GET", "/v1/social/left-out"),
    /** Upserts on network and handle. */
    leaveOut: (body: LeftOutInput) => request<LeftOut>("POST", "/v1/social/left-out", { body }),
    removeLeftOut: (id: number) => request<void>("DELETE", `/v1/social/left-out/${id}`),
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
