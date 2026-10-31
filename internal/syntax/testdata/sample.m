#import <Foundation/Foundation.h>

@interface Point : NSObject
@property (copy) NSString *name;
- (void)moveTo:(int)x;
@end

@implementation Point
- (void)moveTo:(int)x {
    // comment
    NSLog(@"x = %d", x);
}
@end
