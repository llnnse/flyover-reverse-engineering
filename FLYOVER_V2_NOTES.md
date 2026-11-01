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

Layout, as far as known:

```
magic 3d 96 59 2e   u32 version=2   tagged fields (LEB128-style varints?)
f64[9] rotation matrix + f64[3] ECEF translation (same idea as the C3M header)
float fields (scale/bounds?), then a high-entropy block: geometry, codec unknown
3 × embedded HEIF image (ISO-BMFF ftyp…mdat), then 10 trailing bytes:
  #0 "heic" RGB 8-bit, full resolution (e.g. 736×736): albedo texture atlas
  #1 "heix" greyscale, half resolution: material map (mask/occlusion-like)
  #2 "heix" greyscale, half resolution: material map (roughness-like; 204 over water)
```

The RGB image is a UV texture atlas of recognisable aerial imagery (charts with dilated
edges), so **R3D is a PBR-textured mesh format, not Gaussian splats**. This matches the
"PBR Mesh Rendering" debug switch. No splat data has been found in styles 98–100.

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

## Next steps

- Port the C3MM v2 parser to Go and let `fetch-v2` take tile and h values from the metadata octree.
- Decode the R3D geometry block. The codec is unknown, so this likely needs VectorKit.
- Splats: the "PBR Splat Rendering" path in VectorKit may use a style not yet in the manifest,
  or build splats on the device. Look for it in VectorKit.

- The R3D decoder and the splat renderer live in VectorKit/GeoServices, not in Maps.app. Extract
  them from the macOS 27.2 dyld shared cache, then look for `R3D` parsing and the code behind
  `flyoverPbrSplatRendering`.
- Check whether `cmd/auth` signing still applies to V2 URLs.
