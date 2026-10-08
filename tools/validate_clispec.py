#!/usr/bin/env python3
"""Validate `townsquare schema` against the official CLI Spec v0.3 JSON Schema.

Usage: townsquare schema | python3 tools/validate_clispec.py
Needs `pip install jsonschema`. Downloads https://clispec.dev/schema/v0.3.json
(the schema isn't vendored here because its license isn't stated).
"""
import json, sys, urllib.request

import jsonschema

SCHEMA_URL = "https://clispec.dev/schema/v0.3.json"
req = urllib.request.Request(SCHEMA_URL, headers={"User-Agent": "townsquare-ci (+https://github.com/nerveband/townsquare)"})
schema = json.load(urllib.request.urlopen(req, timeout=30))
doc = json.load(sys.stdin)
errors = sorted(jsonschema.Draft202012Validator(schema).iter_errors(doc), key=lambda e: list(e.path))
for e in errors[:20]:
    print("✗", "/".join(map(str, e.path)), e.message[:300])
print(f"clispec v0.3: {len(doc.get('commands', []))} commands, {len(errors)} errors")
sys.exit(1 if errors else 0)
