import { rm, writeFile } from 'node:fs/promises'
import { basename, relative } from 'node:path'
import { build } from 'esbuild'

const production = process.env.NODE_ENV !== 'development'
const shared = {
  bundle: true,
  minify: production,
  sourcemap: production ? 'external' : true,
  target: ['es2020'],
  logLevel: 'info',
}

await rm('dist', { recursive: true, force: true })

const moduleEntries = {
  widget: 'src/widget.ts',
  rating: 'src/modules/rating.ts',
  audio: 'src/modules/audio.ts',
  video: 'src/modules/video.ts',
  replay: 'src/modules/replay.ts',
  'consent-prompt': 'src/consent-prompt.ts',
  screenshot: 'src/modules/screenshot.ts',
  'element-attachment': 'src/modules/element-attachment.ts',
  'error-reporting': 'src/modules/error-reporting.ts',
  'targeting-regex': 'src/modules/targeting-regex.ts',
}

const modules = await build({
  ...shared,
  entryPoints: moduleEntries,
  format: 'esm',
  splitting: true,
  outdir: 'dist/v1/assets',
  entryNames: '[name]-[hash]',
  chunkNames: 'chunks/[name]-[hash]',
  metafile: true,
})

const manifest = {}
for (const [output, metadata] of Object.entries(modules.metafile.outputs)) {
  if (!metadata.entryPoint || !output.endsWith('.js')) continue
  const name = Object.entries(moduleEntries).find(([, source]) => metadata.entryPoint.endsWith(source))?.[0]
  if (name) manifest[name] = relative('dist/v1', output)
}
for (const name of Object.keys(moduleEntries)) {
  const asset = manifest[name]
  if (!asset) throw new Error(`missing widget asset for ${name}`)
  await writeFile(`dist/v1/${name}.js`, `export * from './${asset}'\n`)
}
await writeFile('dist/v1/manifest.json', `${JSON.stringify(manifest, null, 2)}\n`)

await build({
  ...shared,
  entryPoints: ['src/loader.ts'],
  format: 'iife',
  outfile: 'dist/v1/loader.js',
})

console.log(`widget entries: ${Object.values(manifest).map(value => basename(value)).join(', ')}`)
