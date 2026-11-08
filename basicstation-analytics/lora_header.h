/* SPDX-License-Identifier: MIT */
#pragma once
#include <cstddef>
#include <cstdint>
namespace basicstation_analytics {
inline size_t LoRaHeaderLength(const uint8_t* p,size_t n){
 if(!p||!n)return 0;
 switch((p[0]&0xe0)>>5){
 case 0:return n>=23?19:0;
 case 1:return n==17||n==33?n:0;
 case 6:if(n<2)return 0;if(p[1]==1)return n>=24?20:0;if(p[1]==0||p[1]==2)return n>=19?15:0;return 0;
 case 7:return n;
 default:if(n<12)return 0;size_t opts=p[5]&15;if(n<12+opts)return 0;return 8+opts+(n>12+opts?1:0);
 }
}
}
