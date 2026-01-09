#!/usr/bin/env python3
import json
import os
import sys
import urllib.request
import urllib.error


def request_json(method, url, payload=None):
    data = None
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req) as resp:
            body = resp.read().decode("utf-8")
            return json.loads(body) if body else None
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8")
        raise RuntimeError(f"{method} {url} failed: {exc.code} {body}") from exc


def main():
    base_url = os.environ.get("ACTION_GRAPH_API", "http://localhost:8080/api").rstrip("/")

    graphs = request_json("GET", f"{base_url}/action-graphs?include_templates=true")
    if not graphs:
        print("No action graphs found.")
        return 0

    updated = 0
    for item in graphs:
        graph_id = item.get("id")
        if not graph_id:
            continue

        is_template = item.get("is_template", False)
        if is_template:
            detail = request_json("GET", f"{base_url}/templates/{graph_id}")
            update_url = f"{base_url}/templates/{graph_id}"
        else:
            detail = request_json("GET", f"{base_url}/action-graphs/{graph_id}")
            update_url = f"{base_url}/action-graphs/{graph_id}"

        if not detail:
            continue

        payload = {
            "name": detail.get("name", ""),
            "description": detail.get("description") or "",
            "steps": detail.get("steps") or [],
            "preconditions": detail.get("preconditions") or [],
        }

        request_json("PUT", update_url, payload)
        updated += 1
        print(f"Updated {graph_id} ({'template' if is_template else 'graph'})")

    print(f"Done. Updated {updated} action graphs/templates.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
