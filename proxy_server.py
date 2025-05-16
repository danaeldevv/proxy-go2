#!/usr/bin/env python3
import sys
import asyncio

SSH_HOST = '127.0.0.1'
SSH_PORT = 22
BUFFER_SIZE = 8192

async def handle_client(reader, writer):
    try:
        # Peek first few bytes to detect protocol
        reader._transport.settimeout(5)  # Not all asyncio transports support this; we'll approximate below

        # Since asyncio streams don't support peek, we read up to 1024 bytes and then buffer it
        initial_data = await reader.read(1024)
        if not initial_data:
            writer.close()
            await writer.wait_closed()
            return

        # Handle protocol detection
        if initial_data.startswith(b'\x05'):
            # SOCKS5 handshake (simplified)
            # Respond with HTTP/1.1 101 switching protocols (per user request)
            response = b"HTTP/1.1 101 Switching Protocols\r\n\r\n"
            writer.write(response)
            await writer.drain()
        else:
            # HTTP/WebSocket handshake assumed
            # Read remaining HTTP headers if any (simplified)
            request_buffer = initial_data
            while b"\r\n\r\n" not in request_buffer:
                chunk = await reader.read(1024)
                if not chunk:
                    break
                request_buffer += chunk
            response = b"HTTP/1.1 200 OK\r\n\r\n"
            writer.write(response)
            await writer.drain()

        # Connect to OpenSSH server
        ssh_reader, ssh_writer = await asyncio.open_connection(SSH_HOST, SSH_PORT)

        async def forward(src_reader, dest_writer):
            try:
                while True:
                    data = await src_reader.read(BUFFER_SIZE)
                    if not data:
                        break
                    dest_writer.write(data)
                    await dest_writer.drain()
            except Exception:
                pass

        # Start forwarding data bi-directionally
        client_to_ssh = asyncio.create_task(forward(reader, ssh_writer))
        ssh_to_client = asyncio.create_task(forward(ssh_reader, writer))

        await asyncio.wait(
            [client_to_ssh, ssh_to_client],
            return_when=asyncio.FIRST_COMPLETED,
        )

        # Close connections
        ssh_writer.close()
        writer.close()
        await ssh_writer.wait_closed()
        await writer.wait_closed()

    except Exception as e:
        try:
            writer.close()
            await writer.wait_closed()
        except:
            pass

async def run_proxy(port):
    server = await asyncio.start_server(handle_client, '0.0.0.0', port)
    print(f"Proxy server listening on port {port}")

    async with server:
        await server.serve_forever()

if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("Usage: proxy_server.py <port>")
        sys.exit(1)
    try:
        proxy_port = int(sys.argv[1])
        asyncio.run(run_proxy(proxy_port))
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)

