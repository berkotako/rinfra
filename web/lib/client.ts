// RInfraClient interface — seam for future REST wiring.
// All screens render from MockClient; no fetch calls today.
import type { Engagement, CanvasNode, CanvasEdge, C2Framework, Scenario } from "./types";
import { ENGAGEMENTS, INITIAL_NODES, INITIAL_EDGES, C2_FRAMEWORKS, SCENARIOS } from "./data";

export interface RInfraClient {
  listEngagements(): Promise<Engagement[]>;
  getTopology(engagementId: string): Promise<{ nodes: CanvasNode[]; edges: CanvasEdge[] }>;
  listC2Frameworks(): Promise<C2Framework[]>;
  listScenarios(): Promise<Scenario[]>;
}

export class MockClient implements RInfraClient {
  async listEngagements(): Promise<Engagement[]> {
    return ENGAGEMENTS;
  }
  async getTopology(engagementId: string): Promise<{ nodes: CanvasNode[]; edges: CanvasEdge[] }> {
    void engagementId; // future: fetch by engagement
    return { nodes: INITIAL_NODES, edges: INITIAL_EDGES };
  }
  async listC2Frameworks(): Promise<C2Framework[]> {
    return C2_FRAMEWORKS;
  }
  async listScenarios(): Promise<Scenario[]> {
    return SCENARIOS;
  }
}

export const mockClient = new MockClient();
