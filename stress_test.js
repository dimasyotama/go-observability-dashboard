import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');

export const options = {
  stages: [
    { duration: '30s', target: 20 },  // Corrected: Ramp up to 20 users
    { duration: '1m', target: 20 },   // Corrected: Stay at 20 users
    { duration: '30s', target: 50 },  // Corrected: Ramp up to 50 users
    { duration: '1m', target: 50 },   // Corrected: Stay at 50 users
    { duration: '30s', target: 100 }, // Corrected: Spike to 100 users
    { duration: '1m', target: 100 },  // Corrected: Stay at 100 users
    { duration: '30s', target: 0 },   // Ramp down to 0 users
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'], // 95% of requests should be below 500ms
    errors: ['rate<0.1'],              // Error rate should be below 10%
  },
};

const BASE_URL = 'http://localhost:5060';

export default function () {
  // Test root endpoint
  let res = http.get(`${BASE_URL}/`);
  check(res, {
    'root status is 200': (r) => r.status === 200,
  }) || errorRate.add(1);

  // Test status endpoint
  res = http.get(`${BASE_URL}/status`);
  check(res, {
    'status is healthy': (r) => r.status === 200,
    'response time < 200ms': (r) => r.timings.duration < 200,
  }) || errorRate.add(1);

  // Test getting items
  const itemIds = [1, 2, 3];
  const randomItemId = itemIds[Math.floor(Math.random() * itemIds.length)];
  res = http.get(`${BASE_URL}/items/${randomItemId}`);
  check(res, {
    'item retrieval status is 200': (r) => r.status === 200,
    // Safer check: Only parse body if status is 200 and body exists
    'item has name': (r) => r.status === 200 && r.body && JSON.parse(r.body).name !== undefined,
  }) || errorRate.add(1);

  // Test search endpoint
  res = http.get(`${BASE_URL}/search/?name=laptop&min_price=100`);
  check(res, {
    'search status is 200': (r) => r.status === 200,
    // Safer check: Only parse body if status is 200 and body exists
    'search returns results': (r) => r.status === 200 && r.body && JSON.parse(r.body).search_results.length > 0,
  }) || errorRate.add(1);

  // Test creating an item
  const payload = JSON.stringify({
    name: `Test Item ${__VU}-${__ITER}`,
    price: Math.random() * 1000,
    is_offer: Math.random() > 0.5,
  });
  const params = {
    headers: {
      'Content-Type': 'application/json',
    },
  };
  res = http.post(`${BASE_URL}/items/`, payload, params);
  check(res, {
    'item creation status is 200': (r) => r.status === 200,
    // Safer check: Only parse body if status is 200 and body exists
    'item created successfully': (r) => r.status === 200 && r.body && JSON.parse(r.body).message.includes('successfully'),
  }) || errorRate.add(1);

  // Occasionally trigger errors to test error handling
  if (Math.random() < 0.1) {
    res = http.get(`${BASE_URL}/error-400`);
    check(res, {
      'error 400 status is 400': (r) => r.status === 400,
    });
  }

  if (Math.random() < 0.05) {
    res = http.get(`${BASE_URL}/error-500`);
    check(res, {
      'error 500 status is 500': (r) => r.status === 500,
    });
  }

  sleep(Math.random() * 2 + 1); // Random sleep between 1-3 seconds
}

export function handleSummary(data) {
  return {
    'stdout': JSON.stringify(data, null, 2),
    'summary.json': JSON.stringify(data),
  };
}