#!/usr/bin/env python3
"""Test script for running-man - generates various log outputs"""

import sys
import time
import json

def main():
    print("Starting test script...")
    time.sleep(0.5)
    
    # Plain text logs
    print("INFO: Server initialized")
    time.sleep(0.2)
    
    print("WARNING: Using default configuration")
    time.sleep(0.2)
    
    # JSON log
    print(json.dumps({
        "level": "info",
        "message": "Processing request",
        "timestamp": "2024-01-01T12:00:00Z"
    }))
    time.sleep(0.2)
    
    # Error to stderr
    print("ERROR: Database connection failed", file=sys.stderr)
    time.sleep(0.2)
    
    # Python traceback
    try:
        result = 10 / 0
    except Exception as e:
        import traceback
        traceback.print_exc()
    
    time.sleep(0.5)
    print("Test script completed")

if __name__ == "__main__":
    main()
