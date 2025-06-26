#!/usr/bin/env python3
"""
Go Call Graph Analyzer

Analyzes Go source files to build a call graph and identify unused functions.
Creates a DAG (Directed Acyclic Graph) of function calls starting from entry points.
"""

import os
import re
import json
from collections import defaultdict, deque
from pathlib import Path

class GoCallGraphAnalyzer:
    def __init__(self, pkg_dir="pkg", cmd_dir="cmd"):
        self.pkg_dir = pkg_dir
        self.cmd_dir = cmd_dir
        self.functions = {}  # function_name -> {file, line, body}
        self.calls = defaultdict(set)  # caller -> set of callees
        self.entry_points = set()
        
    def parse_go_files(self):
        """Parse all Go files and extract function definitions and calls"""
        print("Parsing Go files...")
        
        # Parse package files
        for go_file in Path(self.pkg_dir).glob("*.go"):
            if go_file.name.endswith("_test.go"):
                continue
            self._parse_file(go_file)
            
        # Parse command files to find entry points
        for go_file in Path(self.cmd_dir).glob("*.go"):
            self._parse_cmd_file(go_file)
    
    def _parse_file(self, file_path):
        """Parse a single Go file for functions and calls"""
        with open(file_path, 'r') as f:
            content = f.read()
            
        # Find function definitions
        # Matches: func (receiver) FunctionName(...) or func FunctionName(...)
        func_pattern = r'func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\([^)]*\)'
        
        for match in re.finditer(func_pattern, content):
            func_name = match.group(1)
            line_num = content[:match.start()].count('\n') + 1
            
            # Get function body (rough approximation)
            start_pos = match.end()
            brace_count = 0
            body_start = -1
            body_end = -1
            
            for i, char in enumerate(content[start_pos:], start_pos):
                if char == '{':
                    if body_start == -1:
                        body_start = i
                    brace_count += 1
                elif char == '}':
                    brace_count -= 1
                    if brace_count == 0 and body_start != -1:
                        body_end = i
                        break
            
            if body_start != -1 and body_end != -1:
                body = content[body_start:body_end + 1]
                self.functions[func_name] = {
                    'file': str(file_path),
                    'line': line_num,
                    'body': body
                }
                
                # Find function calls within this function
                self._extract_calls(func_name, body)
    
    def _parse_cmd_file(self, file_path):
        """Parse command file to find entry points (calls to pkg functions)"""
        with open(file_path, 'r') as f:
            content = f.read()
            
        # Look for calls to dcfh package functions
        # Matches: dcfh.FunctionName( or cache.FunctionName( or similar
        call_pattern = r'(?:dcfh|cache|[a-z_][a-zA-Z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)\s*\('
        
        for match in re.finditer(call_pattern, content):
            func_name = match.group(1)
            if func_name in self.functions:
                self.entry_points.add(func_name)
        
        # Also look for direct function calls that might be entry points
        direct_call_pattern = r'\b([A-Z][A-Za-z0-9_]*)\s*\('
        for match in re.finditer(direct_call_pattern, content):
            func_name = match.group(1)
            if func_name in self.functions:
                self.entry_points.add(func_name)
    
    def _extract_calls(self, caller, body):
        """Extract function calls from a function body"""
        # Look for function calls
        # This is a simplified pattern - may need refinement
        patterns = [
            r'\b([a-z_][a-zA-Z0-9_]*)\s*\(',  # function calls
            r'\.([A-Z][a-zA-Z0-9_]*)\s*\(',   # method calls
            r'dc\.([a-z_][a-zA-Z0-9_]*)\s*\(',  # receiver calls
            r'([A-Z][a-zA-Z0-9_]*)\s*\(',     # exported function calls
        ]
        
        for pattern in patterns:
            for match in re.finditer(pattern, body):
                callee = match.group(1)
                if callee in self.functions and callee != caller:
                    self.calls[caller].add(callee)
    
    def find_reachable_functions(self):
        """Find all functions reachable from entry points using BFS"""
        print(f"Found {len(self.entry_points)} entry points: {sorted(self.entry_points)}")
        
        reachable = set()
        queue = deque(self.entry_points)
        reachable.update(self.entry_points)
        
        while queue:
            current = queue.popleft()
            
            # Add all functions called by current function
            for callee in self.calls.get(current, set()):
                if callee not in reachable:
                    reachable.add(callee)
                    queue.append(callee)
        
        return reachable
    
    def find_unused_functions(self):
        """Find functions that are not reachable from any entry point"""
        reachable = self.find_reachable_functions()
        all_functions = set(self.functions.keys())
        unused = all_functions - reachable
        
        return sorted(unused), sorted(reachable)
    
    def create_dag_dot(self, filename="callgraph.dot", include_unused=False):
        """Create a DOT file for visualizing the call graph as a DAG"""
        reachable = self.find_reachable_functions()
        
        with open(filename, 'w') as f:
            f.write("digraph CallGraph {\n")
            f.write("  rankdir=TB;\n")
            f.write("  node [shape=box];\n")
            
            # Add nodes
            for func in self.functions:
                if include_unused or func in reachable:
                    color = "lightblue" if func in reachable else "lightgray"
                    shape = "diamond" if func in self.entry_points else "box"
                    f.write(f'  "{func}" [fillcolor={color}, style=filled, shape={shape}];\n')
            
            # Add edges
            for caller, callees in self.calls.items():
                if include_unused or caller in reachable:
                    for callee in callees:
                        if include_unused or callee in reachable:
                            f.write(f'  "{caller}" -> "{callee}";\n')
            
            f.write("}\n")
        
        print(f"DOT file created: {filename}")
        print("To visualize: dot -Tpng callgraph.dot -o callgraph.png")
    
    def generate_report(self):
        """Generate a comprehensive report"""
        unused, reachable = self.find_unused_functions()
        
        print(f"\n=== CALL GRAPH ANALYSIS REPORT ===")
        print(f"Total functions found: {len(self.functions)}")
        print(f"Entry points: {len(self.entry_points)}")
        print(f"Reachable functions: {len(reachable)}")
        print(f"Unused functions: {len(unused)}")
        
        print(f"\n=== ENTRY POINTS ===")
        for ep in sorted(self.entry_points):
            print(f"  {ep}")
        
        if unused:
            print(f"\n=== UNUSED FUNCTIONS ===")
            for func in unused:
                file_info = self.functions[func]
                print(f"  {func} ({file_info['file']}:{file_info['line']})")
        
        print(f"\n=== CALL RELATIONSHIPS ===")
        for caller in sorted(self.calls.keys()):
            if self.calls[caller]:
                callees = sorted(self.calls[caller])
                print(f"  {caller} -> {', '.join(callees)}")
        
        # Check for potential cycles (shouldn't exist in proper DAG)
        print(f"\n=== CYCLE DETECTION ===")
        cycles = self._detect_cycles()
        if cycles:
            print("WARNING: Cycles detected (not a true DAG):")
            for cycle in cycles:
                print(f"  {' -> '.join(cycle)}")
        else:
            print("No cycles detected - this is a proper DAG")
    
    def _detect_cycles(self):
        """Detect cycles in the call graph using DFS"""
        visited = set()
        rec_stack = set()
        cycles = []
        
        def dfs(node, path):
            if node in rec_stack:
                # Found a cycle
                cycle_start = path.index(node)
                cycles.append(path[cycle_start:] + [node])
                return
            
            if node in visited:
                return
            
            visited.add(node)
            rec_stack.add(node)
            
            for neighbor in self.calls.get(node, set()):
                dfs(neighbor, path + [node])
            
            rec_stack.remove(node)
        
        for func in self.functions:
            if func not in visited:
                dfs(func, [])
        
        return cycles
    
    def export_json(self, filename="callgraph.json"):
        """Export call graph data as JSON"""
        unused, reachable = self.find_unused_functions()
        
        data = {
            "functions": self.functions,
            "calls": {k: list(v) for k, v in self.calls.items()},
            "entry_points": list(self.entry_points),
            "reachable": reachable,
            "unused": unused,
            "stats": {
                "total_functions": len(self.functions),
                "entry_points_count": len(self.entry_points),
                "reachable_count": len(reachable),
                "unused_count": len(unused)
            }
        }
        
        with open(filename, 'w') as f:
            json.dump(data, f, indent=2)
        
        print(f"JSON export created: {filename}")

def main():
    analyzer = GoCallGraphAnalyzer()
    analyzer.parse_go_files()
    analyzer.generate_report()
    analyzer.create_dag_dot("callgraph.dot", include_unused=True)
    analyzer.create_dag_dot("callgraph_reachable.dot", include_unused=False)
    analyzer.export_json()

if __name__ == "__main__":
    main()