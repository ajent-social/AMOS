import { createServer } from "node:http";

const host = process.env.HOST ?? "127.0.0.1";
const port = Number(process.env.PORT ?? "4175");

function escapeHTML(value) {
  return value.replace(/[&<>"']/g, (character) => {
    switch (character) {
      case "&": return "&amp;";
      case "<": return "&lt;";
      case ">": return "&gt;";
      case '"': return "&quot;";
      case "'": return "&#39;";
      default: return character;
    }
  });
}

const server = createServer((request, response) => {
  const url = new URL(request.url ?? "/", `http://${host}`);
  if (request.method === "GET" && url.pathname === "/health") {
    response.writeHead(200, { "content-type": "text/plain; charset=utf-8" });
    response.end("ready\n");
    return;
  }

  if (request.method === "GET" && url.pathname === "/") {
    const message = escapeHTML(url.searchParams.get("message") ?? "Synthetic browser fixture is ready");
    const html = `<!doctype html>
<html lang="en">
  <head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>AMOS browser fixture</title></head>
  <body>
    <main>
      <h1>AMOS browser fixture</h1>
      <p data-testid="message">${message}</p>
    </main>
  </body>
</html>
`;
    response.writeHead(200, {
      "content-type": "text/html; charset=utf-8",
      "x-content-type-options": "nosniff",
      "x-frame-options": "DENY",
    });
    response.end(html);
    return;
  }

  response.writeHead(404, { "content-type": "text/plain; charset=utf-8" });
  response.end("fixture route not found\n");
});

server.listen(port, host);
server.on("error", (error) => {
  process.stderr.write(`browser fixture server failed to listen: ${error.message}\n`);
  process.exitCode = 1;
});

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => server.close(() => process.exit(0)));
}
