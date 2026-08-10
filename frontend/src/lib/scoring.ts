import type { ScoreWeights } from "../types";

export const DEFAULT_SCORE_WEIGHTS:ScoreWeights={bandwidth:40,https:25,tcp:15,reliability:15,jitter:5};
export const LEGACY_SCORE_WEIGHTS:ScoreWeights={tcp:20,https:20,jitter:10,reliability:30,bandwidth:20};

export function scoreWeightTotal(weights:ScoreWeights){
  return weights.tcp+weights.https+weights.jitter+weights.reliability+weights.bandwidth;
}
