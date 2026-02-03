#!/usr/bin/env python3
"""
Auto-Activate Script for Lifecycle Nodes

Automatically transitions lifecycle nodes through:
  UNCONFIGURED -> configure -> INACTIVE -> activate -> ACTIVE

Usage:
  ros2 run test_action_server auto_activate.py --node /test_a_action_server
  ros2 run test_action_server auto_activate.py --all

This script is useful for development and testing when you want
lifecycle nodes to start in the ACTIVE state automatically.
"""

import argparse
import subprocess
import sys
import time


def get_lifecycle_state(node_name: str) -> str:
    """Get the current lifecycle state of a node."""
    try:
        result = subprocess.run(
            ['ros2', 'lifecycle', 'get', node_name],
            capture_output=True,
            text=True,
            timeout=5
        )
        if result.returncode == 0:
            return result.stdout.strip()
        return "unknown"
    except subprocess.TimeoutExpired:
        return "timeout"
    except Exception as e:
        return f"error: {e}"


def set_lifecycle_state(node_name: str, transition: str) -> bool:
    """Set the lifecycle state of a node."""
    try:
        result = subprocess.run(
            ['ros2', 'lifecycle', 'set', node_name, transition],
            capture_output=True,
            text=True,
            timeout=10
        )
        return result.returncode == 0
    except subprocess.TimeoutExpired:
        print(f"  Timeout while setting {node_name} to {transition}")
        return False
    except Exception as e:
        print(f"  Error setting {node_name} to {transition}: {e}")
        return False


def activate_node(node_name: str, verbose: bool = True) -> bool:
    """Activate a lifecycle node (configure -> activate)."""
    if verbose:
        print(f"\nActivating node: {node_name}")

    # Get current state
    state = get_lifecycle_state(node_name)
    if verbose:
        print(f"  Current state: {state}")

    # Configure if unconfigured
    if "unconfigured" in state.lower():
        if verbose:
            print("  Configuring...")
        if not set_lifecycle_state(node_name, "configure"):
            print(f"  Failed to configure {node_name}")
            return False
        time.sleep(0.5)
        state = get_lifecycle_state(node_name)
        if verbose:
            print(f"  State after configure: {state}")

    # Activate if inactive
    if "inactive" in state.lower():
        if verbose:
            print("  Activating...")
        if not set_lifecycle_state(node_name, "activate"):
            print(f"  Failed to activate {node_name}")
            return False
        time.sleep(0.5)
        state = get_lifecycle_state(node_name)
        if verbose:
            print(f"  State after activate: {state}")

    # Verify active
    if "active" in state.lower():
        if verbose:
            print(f"  Successfully activated {node_name}")
        return True
    else:
        print(f"  Failed to reach active state for {node_name}")
        return False


def list_lifecycle_nodes() -> list:
    """List all available lifecycle nodes."""
    try:
        result = subprocess.run(
            ['ros2', 'lifecycle', 'nodes'],
            capture_output=True,
            text=True,
            timeout=5
        )
        if result.returncode == 0:
            nodes = [n.strip() for n in result.stdout.strip().split('\n') if n.strip()]
            return nodes
        return []
    except Exception:
        return []


def main():
    parser = argparse.ArgumentParser(
        description='Auto-activate lifecycle nodes'
    )
    parser.add_argument(
        '--node', '-n',
        type=str,
        help='Node name to activate (e.g., /test_a_action_server)'
    )
    parser.add_argument(
        '--all', '-a',
        action='store_true',
        help='Activate all lifecycle nodes'
    )
    parser.add_argument(
        '--list', '-l',
        action='store_true',
        help='List all lifecycle nodes'
    )
    parser.add_argument(
        '--wait', '-w',
        type=float,
        default=0,
        help='Wait seconds before starting (useful in launch files)'
    )
    parser.add_argument(
        '--quiet', '-q',
        action='store_true',
        help='Quiet mode (less output)'
    )

    args = parser.parse_args()

    # Wait if requested
    if args.wait > 0:
        print(f"Waiting {args.wait} seconds...")
        time.sleep(args.wait)

    verbose = not args.quiet

    # List nodes
    if args.list:
        nodes = list_lifecycle_nodes()
        if nodes:
            print("Lifecycle nodes:")
            for n in nodes:
                state = get_lifecycle_state(n)
                print(f"  {n}: {state}")
        else:
            print("No lifecycle nodes found")
        return

    # Activate all
    if args.all:
        nodes = list_lifecycle_nodes()
        if not nodes:
            print("No lifecycle nodes found")
            sys.exit(1)

        success = 0
        failed = 0
        for node in nodes:
            if activate_node(node, verbose):
                success += 1
            else:
                failed += 1

        print(f"\nResults: {success} activated, {failed} failed")
        sys.exit(0 if failed == 0 else 1)

    # Activate single node
    if args.node:
        if activate_node(args.node, verbose):
            sys.exit(0)
        else:
            sys.exit(1)

    # No action specified
    parser.print_help()
    sys.exit(1)


if __name__ == '__main__':
    main()
