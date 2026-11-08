/* SPDX-License-Identifier: MIT */
#pragma once
#include <algorithm>
#include <chrono>
#include <condition_variable>
#include <cstdint>
#include <deque>
#include <memory>
#include <mutex>
#include "protobuf/analytics.pb.h"

namespace basicstation_analytics {
class BatchQueue {
 public:
  struct Losses { uint64_t capacity=0, oversize=0, closed=0, pre_identity=0, invalid=0; };
  struct Item { api::AnalyticsMetrics metrics; size_t bytes=0, messages=0; bool sealed=false, in_flight=false; };
  explicit BatchQueue(size_t max_batches=2,size_t max_messages=32,size_t max_bytes=65536,bool require_identity=false,size_t pending_limit=0)
    : max_batches_(std::max<size_t>(1,max_batches)),max_messages_(std::max<size_t>(1,max_messages)),max_bytes_(max_bytes),require_identity_(require_identity),pending_limit_(pending_limit) {}
  static size_t Count(const api::AnalyticsMetrics& m){return m.uplinks_size()+m.downlinks_size()+m.stats_size();}
  bool Add(api::AnalyticsMetrics m,bool seal=false) {
    const size_t n=Count(m),bytes=m.ByteSizeLong()+64; // reserve identity/wrapper overhead
    if(!n)return false;
    std::lock_guard<std::mutex> lock(mu_);
    if(closed_){losses_.closed+=n;++drops_;dropped_messages_+=n;return true;}
    if(n>max_messages_||bytes>max_bytes_){losses_.oversize+=n;++drops_;dropped_messages_+=n;return true;}
    if(require_identity_&&!identity_ready_&&MessagesLocked()+n>pending_limit_){losses_.pre_identity+=n;++drops_;dropped_messages_+=n;return true;}
    bool dropped=false;
    while(true){
      std::shared_ptr<Item> tail;
      if(!q_.empty()&&!q_.back()->sealed&&!q_.back()->in_flight&&q_.back()->messages+n<=max_messages_)tail=q_.back();
      if(bytes_+bytes<=max_bytes_&&(tail||q_.size()<max_batches_)) {
        if(!tail){if(!q_.empty())q_.back()->sealed=true;tail=std::make_shared<Item>();q_.push_back(tail);}
        tail->metrics.MergeFrom(m);tail->bytes+=bytes;tail->messages+=n;bytes_+=bytes;
        tail->sealed=seal||tail->messages>=max_messages_;cv_.notify_all();return dropped;
      }
      auto it=std::find_if(q_.begin(),q_.end(),[](const auto& item){return !item->in_flight;});
      if(it==q_.end()){losses_.capacity+=n;++drops_;dropped_messages_+=n;return true;}
      bytes_-=(*it)->bytes;losses_.capacity+=(*it)->messages;dropped_messages_+=(*it)->messages;++drops_;q_.erase(it);dropped=true;
    }
  }
  bool Push(const api::AnalyticsMetrics& m){return Add(m,true);}
  // Legacy test/helper operation; production sender pins items through Complete.
  bool Pop(api::AnalyticsMetrics* out){std::lock_guard<std::mutex> lock(mu_);if(q_.empty()||!out||q_.front()->in_flight)return false;*out=std::move(q_.front()->metrics);bytes_-=q_.front()->bytes;q_.pop_front();return true;}
  void PushFront(api::AnalyticsMetrics m){Add(std::move(m),true);}
  std::shared_ptr<Item> Next(){std::lock_guard<std::mutex> lock(mu_);if((require_identity_&&!identity_ready_)||q_.empty()||!q_.front()->sealed||q_.front()->in_flight)return {};q_.front()->in_flight=true;return q_.front();}
  void Complete(const std::shared_ptr<Item>& item,bool success,bool discard=false){std::lock_guard<std::mutex> lock(mu_);auto it=std::find(q_.begin(),q_.end(),item);if(it==q_.end())return;if(success||discard){bytes_-=item->bytes;if(discard){losses_.invalid+=item->messages;++drops_;dropped_messages_+=item->messages;}q_.erase(it);}else item->in_flight=false;cv_.notify_all();}
  void SetIdentity(const std::string& id,const std::string& eui){std::lock_guard<std::mutex> lock(mu_);if(identity_ready_)return;for(auto& item:q_){item->metrics.set_gatewayid(id);item->metrics.set_gatewayeui(eui);}identity_ready_=true;cv_.notify_all();}
  void Seal(bool close=false){std::lock_guard<std::mutex> lock(mu_);for(auto& item:q_)item->sealed=true;closed_=closed_||close;
    if(close&&require_identity_&&!identity_ready_){for(auto& item:q_){losses_.pre_identity+=item->messages;++drops_;dropped_messages_+=item->messages;}q_.clear();bytes_=0;}
    cv_.notify_all();}
  void Wait(std::chrono::milliseconds duration){std::unique_lock<std::mutex> lock(mu_);cv_.wait_for(lock,duration);}
  void Wake(){cv_.notify_all();}
  bool Finished()const{std::lock_guard<std::mutex> lock(mu_);return closed_&&q_.empty();}
  size_t Size()const{std::lock_guard<std::mutex> lock(mu_);return q_.size();}
  size_t Bytes()const{std::lock_guard<std::mutex> lock(mu_);return bytes_;}
  size_t Messages()const{std::lock_guard<std::mutex> lock(mu_);size_t n=0;for(auto& item:q_)n+=item->messages;return n;}
  uint64_t Drops()const{std::lock_guard<std::mutex> lock(mu_);return drops_;}
  uint64_t DroppedMessages()const{std::lock_guard<std::mutex> lock(mu_);return dropped_messages_;}
  Losses DropReasons()const{std::lock_guard<std::mutex> lock(mu_);return losses_;}
  void RecordDrop(size_t n){std::lock_guard<std::mutex> lock(mu_);++drops_;dropped_messages_+=n;}
 private:
  size_t MessagesLocked()const{size_t n=0;for(auto& item:q_)n+=item->messages;return n;}
  size_t max_batches_,max_messages_,max_bytes_,bytes_=0;
  mutable std::mutex mu_;std::condition_variable cv_;std::deque<std::shared_ptr<Item>> q_;
  uint64_t drops_=0,dropped_messages_=0;bool closed_=false;
  Losses losses_;
  bool require_identity_=false,identity_ready_=false;size_t pending_limit_=0;
};
}
