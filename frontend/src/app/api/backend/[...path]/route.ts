// This route is a small same-origin proxy for browser-side dashboard requests.
// It forwards GET requests from Next.js to the Go API, avoiding browser CORS
// configuration while keeping the Go API URL on the server side.

type RouteContext = {
  params: Promise<{ path: string[] }>;
};

// GET forwards the requested API path and query string to the Go backend.
// The route is intentionally read-only because Phase 7 only consumes data.
export async function GET(request: Request, { params }: RouteContext) {
  const { path } = await params;
  const apiBaseURL = process.env.MONEYPLANT_API_BASE_URL ?? "http://localhost:8080";
  const normalizedBaseURL = apiBaseURL.replace(/\/$/, "");
  const upstreamURL = `${normalizedBaseURL}/${path.join("/")}${new URL(request.url).search}`;

  // Forward the request to the Go API and preserve its status and JSON body.
  // `cache: no-store` ensures newly ingested candles are visible after refresh.
  const upstreamResponse = await fetch(upstreamURL, {
    cache: "no-store"
  });
  const body = await upstreamResponse.text();

  return new Response(body, {
    status: upstreamResponse.status,
    headers: {
      "Content-Type": upstreamResponse.headers.get("Content-Type") ?? "application/json"
    }
  });
}
