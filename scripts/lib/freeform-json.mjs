#!/usr/bin/env node
/**
 * Types free-form JSON objects in a preprocessed OpenAPI spec as
 * `json.RawMessage` for the Go generator.
 *
 *   node scripts/lib/freeform-json.mjs <in.json> <out.json>
 *
 * The API documents `Map<String, Object>` fields (RunDto
 * `capturePolicySnapshot`, ErrorResponse `details`, …) as
 * `{type: object}` with no properties. oapi-codegen turns those into
 * `map[string]interface{}`, and the provider keeps `interface{}` out of
 * every exported type (doc 42 §5.1.2). A schema node is free-form when it
 * is an object with no `properties`, no `$ref`, no composition keywords
 * and no typed `additionalProperties`; each one gets
 * `x-go-type: json.RawMessage`, so callers decode the value on demand.
 */
import { readFileSync, writeFileSync } from "node:fs";

const [, , input, output] = process.argv;
if (!input || !output) {
  console.error("usage: freeform-json.mjs <in.json> <out.json>");
  process.exit(1);
}

const spec = JSON.parse(readFileSync(input, "utf8"));
let rewritten = 0;

function isFreeForm(node) {
  if (!node || typeof node !== "object" || Array.isArray(node)) return false;
  if (node.$ref || node.allOf || node.oneOf || node.anyOf || node.enum) return false;
  const types = Array.isArray(node.type) ? node.type : [node.type];
  if (!types.includes("object")) return false;
  if (node.properties && Object.keys(node.properties).length > 0) return false;
  const ap = node.additionalProperties;
  if (ap === undefined || ap === true) return true;
  if (ap && typeof ap === "object" && Object.keys(ap).length === 0) return true;
  return false;
}

function walk(node) {
  if (!node || typeof node !== "object") return;
  if (Array.isArray(node)) {
    node.forEach(walk);
    return;
  }
  if (isFreeForm(node) && !node["x-go-type"]) {
    node["x-go-type"] = "json.RawMessage";
    rewritten++;
    return;
  }
  for (const v of Object.values(node)) walk(v);
}

walk(spec.components?.schemas ?? {});
writeFileSync(output, JSON.stringify(spec, null, 2));
console.log(`Typed ${rewritten} free-form object schema(s) as json.RawMessage`);
