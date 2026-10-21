#!/usr/bin/env node
/**
 * CLI entry point for the vendored OpenAPI preprocessor:
 *
 *   node scripts/preprocess.mjs <input.json> <output.json>
 *
 * Invoked by scripts/typegen.sh. Mirrors `devhelm-openapi preprocess` from
 * @devhelm/openapi-tools so CI runs without the (private) monorepo package.
 * The logic lives in scripts/lib/preprocess.mjs, a generated copy of mono's
 * packages/openapi-tools/dist/vendor/preprocess.mjs — re-vendor by copying
 * that file; do not edit it here.
 */
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { preprocessSpec } from './lib/preprocess.mjs';

export * from './lib/preprocess.mjs';

const isMain =
  process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url);

if (isMain) {
  const [, , inputPath, outputPath] = process.argv;
  if (!inputPath || !outputPath) {
    console.error('Usage: node scripts/preprocess.mjs <input.json> <output.json>');
    process.exit(1);
  }
  const spec = JSON.parse(readFileSync(resolve(inputPath), 'utf8'));
  const result = preprocessSpec(spec);
  writeFileSync(resolve(outputPath), JSON.stringify(spec, null, 2), 'utf8');
  const schemaCount = Object.keys(spec.components?.schemas ?? {}).length;
  console.log(`Preprocessed ${schemaCount} schemas → ${resolve(outputPath)}`);
  if (result.flattened.length > 0) {
    console.log(`Flattened circular oneOf: ${result.flattened.join(', ')}`);
  }
  if (result.inlinedDiscriminators.length > 0) {
    console.log(
      `Inlined discriminator subtypes for: ${result.inlinedDiscriminators
        .map((u) => `${u.parent}(${u.discriminator})`)
        .join(', ')}`,
    );
  }
  if (result.inlinedNullableDeductions.length > 0) {
    console.log(
      `Inlined nullable deduction refs for: ${result.inlinedNullableDeductions.join(', ')}`,
    );
  }
  if (result.relaxedEnums.length > 0) {
    console.log(
      `Relaxed response enums (Postel's Law): ${result.relaxedEnums.length} fields`,
    );
  }
}
