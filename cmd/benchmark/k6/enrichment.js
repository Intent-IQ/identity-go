import http from 'k6/http';
import { check } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { SharedArray } from 'k6/data';
import exec from 'k6/execution';

const targetURL = __ENV.TARGET_URL || 'http://127.0.0.1:8081/enrich';
const fixturePath = __ENV.FIXTURE || '../testdata/sample.jsonl';
const mode = (__ENV.MODE || 'sync').toLowerCase();
const iterations = numberFromEnv('ITERATIONS', 1000, 1);
const vus = numberFromEnv('VUS', 16, 1);
const waitMS = numberFromEnv('WAIT_MS', 280, 1);
const hookTimeoutMS = numberFromEnv('HOOK_TIMEOUT_MS', 0, 0);
const tmaxDefaultMS = numberFromEnv('TMAX_DEFAULT_MS', 0, 0);
const errorThreshold = __ENV.ERROR_THRESHOLD || '0.01';

if (!['sync', 'async', 'hybrid'].includes(mode)) {
  throw new Error(`MODE must be sync, async, or hybrid; got ${mode}`);
}

const auctions = new SharedArray('OpenRTB auctions', () =>
  open(fixturePath)
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .map((body) => ({ body, tmax: auctionTMax(body) })),
);

if (auctions.length === 0) {
  throw new Error(`fixture ${fixturePath} contains no requests`);
}

const applicationErrors = new Rate('enrichment_application_errors');
const outcomes = new Counter('enrichment_outcomes');
const auctionDuration = new Trend('enrichment_auction_duration', true);
const delivered = new Counter('enrichment_delivered');
const hookAbandoned = new Counter('enrichment_hook_abandoned');
const auctionBudget = new Trend('enrichment_auction_budget', true);

export const options = {
  scenarios: {
    enrichment: {
      executor: 'shared-iterations',
      vus,
      iterations,
      maxDuration: __ENV.MAX_DURATION || '10m',
    },
  },
  thresholds: {
    checks: ['rate>0.99'],
    http_req_failed: ['rate<0.01'],
    enrichment_application_errors: [`rate<${errorThreshold}`],
  },
};

export default function () {
  const auction = auctions[exec.scenario.iterationInTest % auctions.length];
  const query = mode === 'hybrid'
    ? `mode=${mode}&wait_ms=${waitMS}`
    : `mode=${mode}`;
  const response = http.post(`${targetURL}?${query}`, auction.body, {
    headers: { 'Content-Type': 'application/json' },
    tags: { mode },
  });

  let body;
  try {
    body = response.json();
  } catch (_) {
    body = {};
  }
  const applicationError = Boolean(body.error);
  const duration = response.timings.duration;
  applicationErrors.add(applicationError, { mode });
  auctionDuration.add(duration, { mode });
  outcomes.add(1, { mode, outcome: body.outcome || 'unknown' });

  const abandoned = hookTimeoutMS > 0 && duration > hookTimeoutMS;
  const eids = body.enrichment_result && body.enrichment_result.eids
    ? body.enrichment_result.eids.length
    : 0;
  if (abandoned) {
    hookAbandoned.add(1, { mode });
  }
  if (eids > 0 && !abandoned) {
    delivered.add(1, { mode });
  }
  const tmax = auction.tmax || tmaxDefaultMS;
  if (tmax > 0) {
    const auctionTime = hookTimeoutMS > 0 ? Math.min(duration, hookTimeoutMS) : duration;
    auctionBudget.add(tmax - auctionTime, { mode });
  }

  check(response, {
    'HTTP status is 200': (value) => value.status === 200,
    'response has an outcome': () => typeof body.outcome === 'string' && body.outcome.length > 0,
  }, { mode });
}

function auctionTMax(body) {
  try {
    return Number(JSON.parse(body).tmax) || 0;
  } catch (_) {
    return 0;
  }
}

function numberFromEnv(name, fallback, minimum) {
  const raw = __ENV[name];
  if (raw === undefined || raw === '') return fallback;
  const value = Number(raw);
  if (!Number.isInteger(value) || value < minimum) {
    throw new Error(`${name} must be an integer >= ${minimum}; got ${raw}`);
  }
  return value;
}
