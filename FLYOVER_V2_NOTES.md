# Flyover V2 notes (macOS 27.2 Maps.app)

Static analysis of `Maps.app` 3.0 (CFBundleVersion 2972.21.6.17.31, platform build 26B109)
plus the public resource manifest. No Flyover V2 tiles have been downloaded or decoded yet.

## Tile style IDs

These come from the switch in the tile-style → name function (`__text` 0x100330384, jump
table at 0x100029124, bounds check `cmp w28, #0x77`, arm64 slice). They are checked against
the IDs this repo already uses:

| ID  | Name                       | Repo name |
|-----|----------------------------|-----------|
| 14  | SPUTNIK_METADATA           | C3MM_1    |
| 15  | SPUTNIK_C3M                | C3M       |
| 16  | SPUTNIK_DSM                | DTM_1     |
| 17  | SPUTNIK_DSM_GLOBAL         | DTM_2     |
| 42  | FLYOVER_C3M_MESH           |           |
| 43  | FLYOVER_C3M_JPEG_TEXTURE   |           |
| 44  | FLYOVER_C3M_ASTC_TEXTURE   |           |
| 52  | FLYOVER_METADATA           | C3MM_2    |
| 98  | **FLYOVER_V2_R3D**         | new       |
| 99  | **FLYOVER_V2_DSM**         | new       |
| 100 | **FLYOVER_V2_METADATA**    | new       |

## Resource manifest entries (current clients, os_version=27.2)

The manifest offers 98–100 only to new clients. A query with the old `os_version=11.0.1` stops at 74.

```
style 98  (R3D)       url https://gspe11-ssl.ls.apple.com/tile  version 758  rect x,y 0..2047   z 11..20
style 99  (DSM)       url https://gspe11-ssl.ls.apple.com/tile  version 758  rect x,y 0..16383  z 14
style 100 (METADATA)  url https://gspe11-ssl.ls.apple.com/tile  version 758  rects z 9 (0..511), z 13 (0..8191)
```

Field 5 of `StyleConfig` holds `{1: version, 2: [{1: minX, 2: minY, 3: maxX, 4: maxY, 5: minZ, 6: maxZ}]}`.
V1 C3M used per-region versions from the altitude manifest. V2 appears instead to be a global
XYZ pyramid with a single version, so the URL is probably
`…/tile?style=98&v=758&x=…&y=…&z=…`. This is unverified.

## First fetch results (Santa Monica, v=758)

| Style | URL tried | Result |
|-------|-----------|--------|
| 98 R3D | `?style=98&v=758&x=&y=&z=18` | HTTP 400 for every tile (signing accepted, request malformed) |
| 99 DSM | `?style=99&v=758&x=&y=&z=14` | PNG images (likely elevation) |
| 100 metadata | `?style=100&v=758&x=&y=&z=13` | `C3MM` version 2 files (one empty tile over the ocean) |

## C3MM v2 format (style 100)

```
"C3MM" u16 version=2  u16 2  u32 sectionCount=5
section × 5:
  u8 type  u32 length (incl. this 10-byte header)  u8 compressed  u32 uncompressedSize
  LZMA-alone stream (props 0x5d, dict 1 MiB, size field ff…ff, no end marker:
  patch uncompressedSize into the size field, as for v1)
```

Section types, in file order:

- **0 root grid**: `u8 z(=17) u32 y(TMS) u32 x u8 maxZ(=20) u8 ? u8 ?`, then up to 256 × u32
  entries in Morton order: bits 8–11 = dx, bits 20–23 = dy (0–15) within the zoom-17 block.
  Missing entries mean no data (for example, ocean).
- **8 per root**: `u16 ?` + 8 bytes per root; always `(219, 0)` so far (format or data version?).
- **5 altitude**: `f32 minAlt f32 maxAlt u8 bits(=8)` + per root `u16 lo, u16 hi` quantized.
  The altitudes are ellipsoidal heights (ocean is about −36 m at Santa Monica, which is the geoid offset).
- **1 octree**: `u16 ?` + u16 child masks, one per node, in **depth-first preorder** over the
  roots. They use v1's encoding: 2 bits per child, and child `o` sets x bit `o&1`, y bit `(o>>1)&1`, h bit `(o>>2)&1`.
  The depth is exactly 3 (zoom 17 → 20). About 85 trailing masks are not consumed yet.
- **6 per node**: about 2 bytes per non-root node, small values 0–14 (probably per-node altitude bounds).

So R3D tiles appear to be addressed as (z, x, y, h), like v1 C3M. The missing `h` is the
likely cause of the HTTP 400.

## R3D tiles (style 98)

The working URL is `?style=98&v=758&x=X&y=Y&z=Z&h=H`, with XYZ (top-down) y and `h` from the
metadata octree. Without `h` the server answers HTTP 400. Ten tiles fetched at Santa Monica
(z=17 and z=18, h=0) range from 3.6 KB (ocean) to 424 KB.

Layout: a nested tag–length–value tree with LEB128 varints. It parses exactly to the end of
the file in all ten sample tiles.

```
"3d 96 59 2e"  u32 version=2  01 02 00 01  leb128 (≈ size of the splat block + 160)
node:
  01 01 01 00 01  leb128 len  header (f64[9] rotation + f64[3] ECEF translation + extras)
  tag 4  (24 B)   bounding box: f32 center[3], f32 halfSize[3]
  tag 2           attribute list: 1 byte "00" when empty, else
                  05 01 01 01 02 01 03 07 01 01 06 01 02 04 01 01  (5 attributes?)
  tag 3           SPLAT BLOCK (see below)
  tag 2           child node: textured mesh
    01 01 01 00 01  leb128 len  header (152 B, transform…)
    tag 2         mesh geometry, starts "e1 21 66 24", codec unknown
    tag 3         textures: 3 × HEIF (RGB albedo atlas, 2 half-resolution greyscale material maps)
  tag 3  (4 B)    01 01 01 00
```

### Splat block (top-level tag 3)

```
u32 magic 0x07c922d1 ("d1 22 c9 07")  u32 version=1  u64 count  u32 n
f32 × (1+n) quantization steps; n=8 when there are splats, n=0 for empty blocks:
  0.16 | 0.04 | 0.01 0.01 0.01 | 0.05 0.05 0.05 0.05
  the first step halves with each zoom level (0.32 at z17, 0.16 at z18): position grid step in meters
then compressed per-item data (entropy ≈ 7.98 bits/byte; not LZFSE/zstd/LZMA)
```

Payload sub-header, seen in all six tiles with splats: a LEB128-style varint (not the payload
size), then the constant bytes `25 00`, then fields of varying width (they look bit-packed),
then a recurring marker `80 4a 00 xx 01 xx 06`, a run of zeros containing `24`, and
`02 00 00 e1`. The high-entropy data starts right after that. The same `80 4a 00` marker
also appears inside the mesh node, so the mesh and splat streams may share a codec framework.

| Tile | count | bytes | bytes/item |
|------|-------|-------|------------|
| 18_44784_104710 (pier) | 18239 | 209651 | 11.5 |
| 18_44783_104711 | 4970 | 58061 | 11.7 |
| 18_44784_104711 | 4337 | 51390 | 11.8 |
| 17_22391_52355 | 2164 | 26081 | 12.1 |
| 18_44782_104711 | 808 | 10664 | 13.2 |
| 18_44783_104710 | 504 | 7197 | 14.3 |
| ocean tiles | 0 | 24 | – |

The step groups (1, 1, 3, 4) fit Gaussian-splat attributes: a scalar (opacity?), a scalar
(scale?), RGB colour and a **4-component rotation quaternion**. Positions have no step and are
probably encoded spatially. About 12 bytes per item is typical of compressed 3DGS. **So R3D is
a hybrid: a PBR-textured mesh plus a separate Gaussian-splat payload.** This matches the debug
menu's paired "PBR Mesh Rendering" / "PBR Splat Rendering" switches. The splat codec is unknown
and is probably implemented in VectorKit.

## Splat evidence

The Maps.app debug menu has a "Flyover Rendering" section containing `Bindless Mesh Rendering`,
`PBR Mesh Rendering` and `PBR Splat Rendering`. These map to `VKDebugSettings` properties
`flyoverBindlessMeshRendering`, `flyoverPbrMeshRendering` and `flyoverPbrSplatRendering`, which
means VectorKit has a splat renderer for Flyover. It is not confirmed that `R3D` tiles are splats.

## Tooling

- `pkg/mps/v2.go` decodes StyleConfig field 5 (version + tile ranges). Ranges are expressed at
  their `MinZ` and scale to deeper zooms.
- `cmd/fetch-v2` downloads raw tiles for styles 98–100 using that version and the existing URL
  signing, and logs each response's status, content type, size and first 16 bytes to `index.tsv`.

### What the splats are used for (Central Park vs. Santa Monica, z18, 3×3 tiles each)

| Area | tiles with splats | splats | splats/tile | mesh bytes |
|------|-------------------|--------|-------------|------------|
| Central Park (40.7812, −73.9665) | 9/9 | 118236 | 13137 | 57841 |
| Santa Monica Pier (34.0076, −118.4997) | 5/9 (4 are ocean) | 28858 | 3206 | 91823 |

- Bytes per splat are about 12 in every tile, so the codec rate is stable.
- Within Central Park, a mostly wooded tile (18_77210_98494) has 23291 splats, while the
  ballfields tile next to it (18_77211_98494, open lawn with trees at the edges) has 2840.
  Park meshes are tiny (2–13 KB), so trees are carried by splats, not by the mesh.
- The pier tile (18_44784_104710) has almost no trees but 18239 splats: it holds the amusement
  park (Ferris wheel, roller coaster, rides). So splats also cover thin open structures.
- In short: the mesh carries ground and buildings, and splats carry vegetation and fine
  structures. A rough colour-based canopy estimate correlates only moderately with splat count
  (Spearman 0.66), because grass and water confuse it.

`pkg/fly/r3d` parses the container and extracts the raw splat block, mesh block, textures and a
`meta.json` per tile. `cmd/fetch-v2` does this for each R3D tile it downloads (adding `h=0`), and
`cmd/extract-r3d` does it for tiles already on disk.

## Next steps

- Port the C3MM v2 parser to Go and let `fetch-v2` take tile and h values from the metadata octree.
- Decode the splat block (magic 0x07c922d1) and the mesh block (0x246621e1). Both codecs are
  unknown and are probably implemented in VectorKit (macOS 27.2 dyld shared cache).
- No style in the 0–119 enum is splat-specific (103–119 are UNUSED), so splats only come in R3D.

- The R3D decoder and the splat renderer live in VectorKit/GeoServices, not in Maps.app. Extract
  them from the macOS 27.2 dyld shared cache, then look for `R3D` parsing and the code behind
  `flyoverPbrSplatRendering`.
- Check whether `cmd/auth` signing still applies to V2 URLs.
