import type { Mode, ScoreWeights } from "../types";

export const DEFAULT_SCORE_WEIGHTS:ScoreWeights={bandwidth:40,http:25,connection:15,reliability:15,jitter:5};
export const LEGACY_SCORE_WEIGHTS:ScoreWeights={tcp:20,https:20,jitter:10,reliability:30,bandwidth:20};
export const H3_LEGACY_SCORE_WEIGHTS:ScoreWeights={bandwidth:40,http3:25,quic:15,reliability:15,jitter:5};
export const connectionWeight=(weights:ScoreWeights,mode?:Mode)=>weights.connection??(mode==="h3"?weights.quic:weights.tcp)??0;
export const httpWeight=(weights:ScoreWeights,mode?:Mode)=>weights.http??(mode==="h3"?weights.http3:weights.https)??0;
export function scoreWeightTotal(weights:ScoreWeights){return Object.values(weights).reduce((sum,value)=>sum+(value??0),0);}
