// Stands in for Caddy: /api and /ws go to the hub, everything else to the Next.js dev server.
import http from "node:http";
import net from "node:net";

const [listen, hub, web] = process.argv.slice(2).map(Number);
const toHub = (url) => url.startsWith("/api/") || url.startsWith("/ws/");

const server = http.createServer((req, res) => {
  const port = toHub(req.url) ? hub : web;
  const up = http.request({ host: "127.0.0.1", port, path: req.url, method: req.method, headers: req.headers }, (r) => {
    res.writeHead(r.statusCode, r.headers);
    r.pipe(res);
  });
  up.on("error", () => res.writeHead(502).end());
  req.pipe(up);
});

server.on("upgrade", (req, socket, head) => {
  const up = net.connect(toHub(req.url) ? hub : web, "127.0.0.1", () => {
    const lines = [`${req.method} ${req.url} HTTP/1.1`];
    for (let i = 0; i < req.rawHeaders.length; i += 2) lines.push(`${req.rawHeaders[i]}: ${req.rawHeaders[i + 1]}`);
    up.write(lines.join("\r\n") + "\r\n\r\n");
    up.write(head);
    socket.pipe(up).pipe(socket);
  });
  up.on("error", () => socket.destroy());
  socket.on("error", () => up.destroy());
});

server.listen(listen, "127.0.0.1");
