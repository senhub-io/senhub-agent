#!/usr/bin/env python3

import json
import subprocess
import sys
from datetime import datetime

def extract_redfish_metrics(cache_file_path):
    """Extract redfish metrics from cache.log"""
    
    print(f"Reading cache data from {cache_file_path}")
    
    # Read cache file
    try:
        with open(cache_file_path, 'r') as f:
            cache_data = json.load(f)
    except Exception as e:
        print(f"Error reading cache file: {e}")
        return None
    
    # Extract only redfish metrics
    redfish_metrics = []
    for entry in cache_data.get("entries", []):
        if entry.get("probe_name") == "redfish":
            # Convert to the format expected by our injection endpoint
            redfish_metrics.append({
                "name": entry["name"],
                "value": entry["value"],
                "unit": entry.get("unit", ""),
                "timestamp": entry["timestamp"],
                "tags": entry["tags"]
            })
    
    print(f"Found {len(redfish_metrics)} redfish metrics")
    
    if not redfish_metrics:
        print("No redfish metrics found in cache")
        return None
    
    # Show a few examples
    print("\nExample metrics:")
    for i, metric in enumerate(redfish_metrics[:10]):
        tags_str = ", ".join([f"{k}:{v}" for k, v in metric["tags"].items() if k in ["pool_name", "volume_name", "drive_name", "controller", "fan_name"]])
        print(f"  {i+1}. {metric['name']} - {tags_str}")
    
    # Prepare injection payload
    injection_data = {
        "metrics": redfish_metrics,
        "source": "production_cache_injection"
    }
    
    # Save to temporary file for curl
    with open('temp_injection.json', 'w') as f:
        json.dump(injection_data, f, indent=2)
    
    print(f"\nSaved {len(redfish_metrics)} metrics to temp_injection.json")
    return injection_data

def inject_with_curl(agent_url, agent_key):
    """Use curl to inject the data"""
    inject_url = f"{agent_url}/api/{agent_key}/debug/inject-real-metrics"
    
    curl_cmd = [
        'curl', '-s', '-X', 'POST',
        '-H', 'Content-Type: application/json',
        '-d', '@temp_injection.json',
        inject_url
    ]
    
    try:
        print(f"Injecting via curl to {inject_url}")
        result = subprocess.run(curl_cmd, capture_output=True, text=True, check=True)
        
        response = json.loads(result.stdout)
        print(f"✅ Successfully injected {response.get('metrics_count', 0)} metrics")
        
        print(f"\n🔗 Test URLs:")
        for name, url in response.get('test_urls', {}).items():
            print(f"   {name}: {url}")
        
        print(f"\n🎯 Contextual filtering examples:")
        for name, url in response.get('contextual_filtering_examples', {}).items():
            print(f"   {name}: {url}")
        
        # Clean up temp file
        subprocess.run(['rm', 'temp_injection.json'], check=False)
        
        return True
        
    except subprocess.CalledProcessError as e:
        print(f"❌ Curl injection failed: {e}")
        print(f"Error output: {e.stderr}")
        return False
    except Exception as e:
        print(f"❌ Error during injection: {e}")
        return False

if __name__ == "__main__":
    cache_file = "/Users/matthieu/Downloads/cache.log"
    agent_url = "http://localhost:8080"
    agent_key = "2a5d71c5-706e-43ce-8a10-8ee252f85772"
    
    # Extract metrics
    injection_data = extract_redfish_metrics(cache_file)
    if injection_data is None:
        sys.exit(1)
    
    # Inject with curl
    success = inject_with_curl(agent_url, agent_key)
    sys.exit(0 if success else 1)