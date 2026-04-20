#!/usr/bin/env node
/**
 * Standalone CLI wrapper around the vendored preprocessor in lib/preprocess.mjs.
 * Mirrors the `devhelm-openapi preprocess <input> <output>` interface so we can
 * invoke it from `typegen.sh` without depending on the (private) npm package.
 */
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { preprocessSpec } from './lib/preprocess.mjs'

const [, , inputArg, outputArg] = process.argv
if (!inputArg || !outputArg) {
  console.error('usage: preprocess.mjs <input.json> <output.json>')
  process.exit(1)
}

const inputPath = resolve(inputArg)
const outputPath = resolve(outputArg)

const spec = JSON.parse(readFileSync(inputPath, 'utf8'))
const { flattened, inlinedDiscriminators } = preprocessSpec(spec)

const schemaCount = Object.keys(spec.components?.schemas ?? {}).length
console.log(`Preprocessed ${schemaCount} schemas → ${outputPath}`)

if (flattened.length > 0) {
  console.log(`Flattened circular oneOf: ${flattened.join(', ')}`)
}
if (inlinedDiscriminators.length > 0) {
  console.log(
    `Inlined discriminator subtypes for: ${inlinedDiscriminators
      .map((u) => `${u.parent}(${u.discriminator})`)
      .join(', ')}`,
  )
}

writeFileSync(outputPath, JSON.stringify(spec, null, 2))
