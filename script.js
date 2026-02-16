import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

// Custom metrics
export let errorRate = new Rate('errors');
export let responseTime = new Trend('response_time');
export let responseSize = new Trend('response_size');

export let options = {
  stages: [
    { duration: '30s', target: 100 }, // Ramp-up to 100 users
    { duration: '1m', target: 100 },  // Hold 100 users
    { duration: '30s', target: 10 },  // Ramp-down to 10 users
  ],
  thresholds: {
    errors: ['rate<0.01'],  // Less than 1% error rate
    http_req_duration: ['p(95)<500'], // 95% of requests should be < 500ms
    'response_size': ["max<160000"], // 95% of responses should be < 100KB (fix)
  },
};

export default function () {
  const url = 'https://precastezy.blueinvent.com/api/dashboard_element';
  const params = {
    headers: {
      'Authorization': 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJlbWFpbCI6ImNvb2xkdWRlMTY2M0BnbWFpbC5jb20iLCJleHAiOjE3Mzg2NTE2NzZ9.FEH6eLUy3dPcH6SmjqtVNe-iaa9w2YzpSFQVl1MC6c8',
      'Content-Type': 'application/json',
    },
  };

  let res = http.get(url, params);
  
  // Add response time and response size metrics
  responseTime.add(res.timings.duration);
  responseSize.add(res.body.length);

  // Log response size in bytes
  console.log(`Response Size: ${res.body.length} bytes`);

  // Validate response
  check(res, {
    'status is 200': (r) => r.status === 200,
  }) || errorRate.add(1);

  sleep(1); // Simulate user delay before next request
}
