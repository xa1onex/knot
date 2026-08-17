# @node-infra/client

Stage 5.0 **Node Client SDK** (TypeScript) for the Web shell (and any script that speaks `/v1`).

```text
Web / script → NodeClient → Node API /v1 → Storage / Devices / Transfers
```

No private transfer protocol. Bytes always go through Node Network (Direct|Relay) via agents.

## Install (workspace)

```bash
cd sdk/js && npm install && npm run build
```

## Usage

```ts
import { NodeClient, AppScopes, isQuotaExceeded } from '@node-infra/client'

const cl = new NodeClient({ baseUrl: 'http://127.0.0.1:8787' })
await cl.login('admin@node.local', 'admin')

const devices = await cl.listDevices()
const home = devices.find((d) => d.online)
const entries = await cl.storageList(home!.id, 'photos')

const up = await cl.storageUpload({
  device_id: home!.id,
  path: 'photos/shot.jpg',
  from_device_id: /* source agent */,
  source_path: 'shot.jpg',
  size: 1234,
  sha256: '...',
})
await cl.watchTransfer(up.id, {
  onProgress: (p) => console.log(p.percent.toFixed(1) + '%'),
})
```

See [docs/client-sdk.md](../../docs/client-sdk.md).
