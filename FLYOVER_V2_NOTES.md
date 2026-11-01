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

## Splat evidence

The Maps.app debug menu has a "Flyover Rendering" section containing `Bindless Mesh Rendering`,
`PBR Mesh Rendering` and `PBR Splat Rendering`. These map to `VKDebugSettings` properties
`flyoverBindlessMeshRendering`, `flyoverPbrMeshRendering` and `flyoverPbrSplatRendering`, which
means VectorKit has a splat renderer for Flyover. It is not confirmed that `R3D` tiles are splats.

## Next steps

- The R3D decoder and the splat renderer live in VectorKit/GeoServices, not in Maps.app. Extract
  them from the macOS 27.2 dyld shared cache, then look for `R3D` parsing and the code behind
  `flyoverPbrSplatRendering`.
- Check whether `cmd/auth` signing still applies to V2 URLs.
